package services

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/go-gota/gota/dataframe"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	kafka_internal "github.com/redhatinsights/ros-ocp-backend/internal/kafka"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
	"github.com/redhatinsights/ros-ocp-backend/internal/types/kruizePayload"
	namespacePayload "github.com/redhatinsights/ros-ocp-backend/internal/types/kruizePayload/namespace"
	w "github.com/redhatinsights/ros-ocp-backend/internal/types/workload"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils/kruize"
)

// parallelIngestFiles processes manifest files concurrently with bounded parallelism.
// Returns a transient error (if any goroutine hit one — triggers Kafka retry) and
// a bool indicating whether any file had a permanent failure.
func parallelIngestFiles(
	ctx context.Context,
	pool *pgxpool.Pool,
	log *logrus.Entry,
	kafkaMsg types.KafkaMsg,
	manifestID string,
	useNativeCSVIngest bool,
	rhAccount *model.RHAccount,
	cluster *model.Cluster,
) (transientErr error, permanentFailed bool) {
	// Files carry their original indices throughout (filename derivation
	// reads Object_keys by index).
	remaining := make([]vmFamilyFile, 0, len(kafkaMsg.Files))
	for i, f := range kafkaMsg.Files {
		remaining = append(remaining, vmFamilyFile{idx: i, file: f})
	}

	if useNativeCSVIngest {
		ingestion.EnsureIngestPartitionsForWindow(ctx, pool)

		// #707: VM-family files of one payload digest in a single batch —
		// per-file passes clobbered same-day rows split across files (the
		// full-replacement upsert kept only the last file's rows). Instance-
		// types files and every other type keep the existing per-file paths.
		if plugin.EnabledFor("vm") {
			vmFiles, otherFiles := partitionVMFamilyFiles(remaining)
			if len(vmFiles) > 0 {
				tErr, pErr := processVMFileBatch(ctx, pool, log, kafkaMsg, manifestID, vmFiles)
				if tErr != nil {
					return tErr, permanentFailed
				}
				if pErr {
					permanentFailed = true
				}
				remaining = otherFiles
				if len(remaining) == 0 {
					return nil, permanentFailed
				}
			}
		}
	}

	appCfg := config.GetConfig()
	workers := appCfg.ManifestDownloadWorkers
	if workers <= 0 {
		workers = 2
	}

	// For a single file, skip the goroutine overhead entirely.
	if len(remaining) <= 1 {
		for _, f := range remaining {
			tErr, pErr := processOneFile(ctx, pool, log, kafkaMsg, manifestID, useNativeCSVIngest, rhAccount, cluster, f.idx, f.file)
			if tErr != nil {
				return tErr, permanentFailed
			}
			if pErr {
				permanentFailed = true
			}
		}
		return nil, permanentFailed
	}

	// Use errgroup with bounded concurrency. A transient error cancels remaining work
	// via the derived context. Permanent errors are tracked separately.
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(workers)

	var permFailed atomic.Bool
	var transientOnce sync.Once
	var firstTransient error

	for _, f := range remaining {
		f := f
		eg.Go(func() error {
			tErr, pErr := processOneFile(egCtx, pool, log, kafkaMsg, manifestID, useNativeCSVIngest, rhAccount, cluster, f.idx, f.file)
			if pErr {
				permFailed.Store(true)
			}
			if tErr != nil {
				transientOnce.Do(func() { firstTransient = tErr })
				return tErr // cancels egCtx so other goroutines stop
			}
			return nil
		})
	}

	_ = eg.Wait()
	return firstTransient, permFailed.Load()
}

// vmFamilyFile is a manifest file routed to the VM digest batch, carrying
// its original index for filename derivation.
type vmFamilyFile struct {
	idx  int
	file string
}

// partitionVMFamilyFiles splits the manifest's files into VM-family members
// (usage / gpu-device / pvc, excluding the instance-types JSON) and the
// rest. Classification mirrors processNativeFile's routing.
func partitionVMFamilyFiles(files []vmFamilyFile) (vmFiles, otherFiles []vmFamilyFile) {
	for _, f := range files {
		if vm.IsClusterInstanceTypesFile(f.file) {
			otherFiles = append(otherFiles, f)
			continue
		}
		switch utils.DetermineCSVType(f.file) {
		case types.PayloadTypeVM, types.PayloadTypeVMGPU, types.PayloadTypeVMPVC:
			vmFiles = append(vmFiles, f)
		default:
			otherFiles = append(otherFiles, f)
		}
	}
	return vmFiles, otherFiles
}

// processVMFileBatch digests all VM-family files of one payload in a single
// accumulator (#707): same-day rows across files merge; per-file manifest
// state and error isolation are preserved (a failed file takes the
// per-file error path; batch-mates keep their rows). Cross-payload
// replacement semantics are unchanged (KMO's monthly accumulator).
func processVMFileBatch(
	ctx context.Context,
	pool *pgxpool.Pool,
	log *logrus.Entry,
	kafkaMsg types.KafkaMsg,
	manifestID string,
	vmFiles []vmFamilyFile,
) (transientErr error, permanentFailed bool) {
	if !plugin.EnabledFor("vm") {
		// #650: skipping a disabled plugin's files is Done, not silence.
		for _, f := range vmFiles {
			filename := filenameForFileIndex(kafkaMsg, f.file, f.idx)
			log.Warnf("vm plugin disabled, skipping %s without ingest (marking done)", filename)
			markFileDone(ctx, pool, log, manifestID, filename)
		}
		return nil, false
	}

	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	batch, err := ingestion.NewVMDigestBatch(ctx, pool, orgID, clusterUUID)
	if err != nil {
		// Schedule-cache load failure is environmental: transient, nothing marked done.
		log.Errorf("VM batch setup failed: %v", err)
		return err, false
	}

	// Succeeded files are marked done only after the flush: a failed flush
	// leaves them unmarked so the manifest retry re-processes them
	// (ingest is idempotent).
	var succeeded []string
	for _, f := range vmFiles {
		if err := ctx.Err(); err != nil {
			return err, false
		}
		filename := filenameForFileIndex(kafkaMsg, f.file, f.idx)
		if shouldSkipProcessedFile(ctx, pool, manifestID, filename) {
			log.Infof("skipping already-processed file %s for manifest %s", filename, manifestID)
			continue
		}
		markFileProcessing(ctx, pool, log, kafkaMsg, filename, string(types.PayloadTypeVM))
		err := ingestCSVFromURL(ctx, f.file, orgID, clusterUUID, string(types.PayloadTypeVM),
			func(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
				_, err := batch.AddCSV(ctx, r)
				return err
			})
		if err != nil {
			if isTransientKafkaProcessingError(err) {
				return err, false
			}
			handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, string(types.PayloadTypeVM), err)
			permanentFailed = true
			continue
		}
		succeeded = append(succeeded, filename)
	}

	if err := batch.Flush(ctx); err != nil {
		if isTransientKafkaProcessingError(err) {
			return err, false
		}
		// Permanent flush failure fails the whole batch's succeeded files:
		// none were marked done, and their data did not land.
		for _, filename := range succeeded {
			handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, string(types.PayloadTypeVM), err)
		}
		return nil, true
	}
	for _, filename := range succeeded {
		markFileDone(ctx, pool, log, manifestID, filename)
	}
	return nil, permanentFailed
}

// processOneFile handles a single file from the manifest. Returns a transient error
// (should cancel the group) or a permanent failure indicator.
func processOneFile(
	ctx context.Context,
	pool *pgxpool.Pool,
	log *logrus.Entry,
	kafkaMsg types.KafkaMsg,
	manifestID string,
	useNativeCSVIngest bool,
	rhAccount *model.RHAccount,
	cluster *model.Cluster,
	fileIdx int,
	file string,
) (transientErr error, permanentFailed bool) {
	if err := ctx.Err(); err != nil {
		return err, false
	}

	filename := filenameForFileIndex(kafkaMsg, file, fileIdx)
	if shouldSkipProcessedFile(ctx, pool, manifestID, filename) {
		log.Infof("skipping already-processed file %s for manifest %s", filename, manifestID)
		return nil, false
	}

	// Native ingest path (plugin-based engine).
	if useNativeCSVIngest {
		return processNativeFile(ctx, pool, log, kafkaMsg, manifestID, filename, file)
	}

	// Legacy Kruize fallback path.
	return processLegacyFile(log, kafkaMsg, file, rhAccount, cluster)
}

func processNativeFile(
	ctx context.Context,
	pool *pgxpool.Pool,
	log *logrus.Entry,
	kafkaMsg types.KafkaMsg,
	manifestID, filename, file string,
) (transientErr error, permanentFailed bool) {
	if plugin.EnabledFor("vm") && vm.IsClusterInstanceTypesFile(file) {
		reportType := "cluster_instance_types"
		markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
		if err := processClusterInstanceTypesIngest(ctx, file, kafkaMsg); err != nil {
			if isTransientKafkaProcessingError(err) {
				return err, false
			}
			handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
			return nil, true
		}
		markFileDone(ctx, pool, log, manifestID, filename)
		return nil, false
	}

	csvType := utils.DetermineCSVType(file)
	reportType := string(csvType)
	if csvType == types.PayloadTypeUnknown {
		log.Infof("skipping unrecognized file %s (type %s)", filename, reportType)
		markFileDone(ctx, pool, log, manifestID, filename)
		return nil, false
	}

	switch csvType {
	case types.PayloadTypeContainer:
		markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
		if err := processContainerCSVIngest(ctx, file, kafkaMsg); err != nil {
			if isTransientKafkaProcessingError(err) {
				return err, false
			}
			handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
			return nil, true
		}
		markFileDone(ctx, pool, log, manifestID, filename)

	case types.PayloadTypeNamespace:
		markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
		if err := processNamespaceCSVIngest(ctx, file, kafkaMsg); err != nil {
			if isTransientKafkaProcessingError(err) {
				return err, false
			}
			handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
			return nil, true
		}
		markFileDone(ctx, pool, log, manifestID, filename)

	case types.PayloadTypeStorage:
		markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
		if err := processStorageCSVIngest(ctx, file, kafkaMsg); err != nil {
			if isTransientKafkaProcessingError(err) {
				return err, false
			}
			handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
			return nil, true
		}
		markFileDone(ctx, pool, log, manifestID, filename)

	case types.PayloadTypeSnapshot:
		markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
		if err := processSnapshotCSVIngest(ctx, file, kafkaMsg); err != nil {
			if isTransientKafkaProcessingError(err) {
				return err, false
			}
			handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
			return nil, true
		}
		markFileDone(ctx, pool, log, manifestID, filename)

	case types.PayloadTypeSLO:
		// #644 load-bearing: SLO ingest never permanently fails
		// (skip-with-counters, always Done). A poisoned file must not gate
		// container recommendations via manifest completeness. Transient
		// fetch errors still retry; all other errors warn + mark Done.
		markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
		if err := processSLOCSVIngest(ctx, file, kafkaMsg); err != nil {
			if isTransientKafkaProcessingError(err) {
				return err, false
			}
			log.Warnf("slo ingestion failed (non-fatal, marking done): %v", err)
			IngestionFileFailures.WithLabelValues(reportType, "slo-nonfatal").Inc()
			markFileDone(ctx, pool, log, manifestID, filename)
			return nil, false
		}
		markFileDone(ctx, pool, log, manifestID, filename)

	case types.PayloadTypeAPITax:
		// Thin W5 (#393): same never-fails contract as SLO — warn +
		// Done, never Failed, so a poisoned file cannot stall a manifest
		// (the #650 default guard would also catch it, but explicit beats
		// silent for a known type).
		markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
		if err := processAPITaxCSVIngest(ctx, file, kafkaMsg); err != nil {
			if isTransientKafkaProcessingError(err) {
				return err, false
			}
			log.Warnf("apitax ingestion failed (non-fatal, marking done): %v", err)
			IngestionFileFailures.WithLabelValues(reportType, "apitax-nonfatal").Inc()
			markFileDone(ctx, pool, log, manifestID, filename)
			return nil, false
		}
		markFileDone(ctx, pool, log, manifestID, filename)

	case types.PayloadTypeNodepool:
		// #673 (Child A of #660): same never-fails contract as SLO/apitax
		// — warn + Done, never Failed, so a poisoned file cannot stall
		// a manifest.
		markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
		if err := processNodepoolCSVIngest(ctx, file, kafkaMsg); err != nil {
			if isTransientKafkaProcessingError(err) {
				return err, false
			}
			log.Warnf("nodepool ingestion failed (non-fatal, marking done): %v", err)
			IngestionFileFailures.WithLabelValues(reportType, "nodepool-nonfatal").Inc()
			markFileDone(ctx, pool, log, manifestID, filename)
			return nil, false
		}
		markFileDone(ctx, pool, log, manifestID, filename)

	case types.PayloadTypeHCPCapacity:
		// W4 fleet-capacity snapshots (#392): same never-fails contract as
		// SLO/apitax/nodepool — warn + Done, never Failed.
		markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
		if err := processHCPCapacityCSVIngest(ctx, file, kafkaMsg); err != nil {
			if isTransientKafkaProcessingError(err) {
				return err, false
			}
			log.Warnf("hcp-capacity ingestion failed (non-fatal, marking done): %v", err)
			IngestionFileFailures.WithLabelValues(reportType, "hcp-capacity-nonfatal").Inc()
			markFileDone(ctx, pool, log, manifestID, filename)
			return nil, false
		}
		markFileDone(ctx, pool, log, manifestID, filename)

	case types.PayloadTypeClusterQuota:
		markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
		if err := processClusterQuotaCSVIngest(ctx, file, kafkaMsg); err != nil {
			if isTransientKafkaProcessingError(err) {
				return err, false
			}
			handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
			return nil, true
		}
		markFileDone(ctx, pool, log, manifestID, filename)

	case types.PayloadTypeVM, types.PayloadTypeVMGPU, types.PayloadTypeVMPVC:
		if plugin.EnabledFor("vm") {
			markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
			if err := processVMCsvIngest(ctx, file, kafkaMsg, csvType); err != nil {
				if isTransientKafkaProcessingError(err) {
					return err, false
				}
				handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
				return nil, true
			}
			markFileDone(ctx, pool, log, manifestID, filename)
		} else {
			// #650: skipping a disabled plugin's files is Done, not silence.
			// The fall-through used to stall the whole manifest; VM engines
			// already no-op when disabled, so recs proceed without VM data.
			log.Warnf("vm plugin disabled, skipping %s without ingest (marking done)", filename)
			markFileDone(ctx, pool, log, manifestID, filename)
		}

	default:
		// #650 fail-safe: a recognized type with no case above must never
		// stall a manifest. Warn + Done mirrors the Unknown branch: a file
		// that can never succeed must not retry-loop forever as Failed.
		log.Warnf("unhandled recognized file type %s for %s — marking done without ingest", reportType, filename)
		markFileDone(ctx, pool, log, manifestID, filename)
	}

	return nil, false
}

func processLegacyFile(
	log *logrus.Entry,
	kafkaMsg types.KafkaMsg,
	file string,
	rhAccount *model.RHAccount,
	cluster *model.Cluster,
) (transientErr error, permanentFailed bool) {
	appCfg := config.GetConfig()
	csvType := utils.DetermineCSVType(file)
	if csvType == types.PayloadTypeUnknown {
		return nil, false
	}

	data, fetchError := utils.ReadCSVFromUrl(file)
	if fetchError != nil {
		csvFetchError.Inc()
		log.Errorf("unable to read CSV from URL: %s", fetchError.Error())
		if isTransientKafkaProcessingError(fetchError) {
			return fetchError, false
		}
		return nil, true
	}
	columnHeaders := types.GetColumnMapping(csvType)
	df := dataframe.LoadRecords(data, dataframe.WithTypes(columnHeaders))
	df, parseError := utils.Aggregate_data(csvType, df)
	if parseError != nil {
		log.Errorf("unable to process %s; error: %s ", file, parseError.Error())
		ingestionErrors.WithLabelValues("csv_parse").Inc()
		switch csvType {
		case types.PayloadTypeNamespace:
			invalidNamespaceCSV.Inc()
		case types.PayloadTypeContainer:
			invalidCSV.Inc()
		}
		return nil, true
	}

	switch csvType {
	case types.PayloadTypeContainer:
		k8s_object_groups := df.GroupBy("namespace", "k8s_object_type", "k8s_object_name").GetGroups()
		for _, v := range k8s_object_groups {
			all_interval_end_time := v.Col("interval_end").Records()
			maxEndTime, err := utils.MaxIntervalEndTime(all_interval_end_time)
			if err != nil {
				log.Errorf("unable to convert string to time: %s", err)
				continue
			}

			k8s_object := v.Maps()
			namespace := kruizePayload.AssertAndConvertToString(k8s_object[0]["namespace"])
			k8s_object_type := k8s_object[0]["k8s_object_type"].(string)
			k8s_object_name := k8s_object[0]["k8s_object_name"].(string)

			experiment_name := utils.GenerateExperimentName(
				kafkaMsg.Metadata.Org_id,
				kafkaMsg.Metadata.Source_id,
				kafkaMsg.Metadata.Cluster_uuid,
				namespace,
				k8s_object_type,
				k8s_object_name,
			)

			cluster_identifier := kafkaMsg.Metadata.Org_id + ";" + kafkaMsg.Metadata.Cluster_uuid
			container_names, err := kruize.Create_kruize_experiments(experiment_name, cluster_identifier, k8s_object)
			if err != nil {
				log.Error(err)
				continue
			}

			workload := model.Workload{
				OrgId:           rhAccount.OrgId,
				ClusterID:       cluster.ID,
				ExperimentName:  experiment_name,
				Namespace:       namespace,
				WorkloadType:    w.WorkloadType(k8s_object_type),
				WorkloadName:    k8s_object_name,
				Containers:      container_names,
				MetricsUploadAt: maxEndTime,
			}
			if err := workload.CreateWorkload(); err != nil {
				log.Errorf("unable to save workload record: %v. Error: %v", workload, err)
				if isTransientKafkaProcessingError(err) {
					return err, false
				}
				continue
			}

			var k8s_object_chunks [][]kruizePayload.UpdateResult
			update_result_payload_data := kruizePayload.GetUpdateResultPayload(experiment_name, k8s_object)
			if len(update_result_payload_data) > appCfg.KruizeMaxBulkChunkSize {
				k8s_object_chunks = SliceMetricsUpdatePayloadToChunks(update_result_payload_data)
			} else {
				k8s_object_chunks = append(k8s_object_chunks, update_result_payload_data)
			}

			for _, chunk := range k8s_object_chunks {
				usage_data_byte, err := kruize.Update_results(experiment_name, chunk)
				if err != nil {
					log.Error(err, experiment_name)
					continue
				}

				workload_metric_arr := []model.WorkloadMetrics{}
				for _, data := range usage_data_byte {
					interval_start_time, err := utils.ConvertISO8601StringToTime(data.Interval_start_time)
					if err != nil {
						log.Errorf("Error for start time: %s", err)
						continue
					}
					interval_end_time, err := utils.ConvertISO8601StringToTime(data.Interval_end_time)
					if err != nil {
						log.Errorf("Error for end time: %s", err)
						continue
					}

					for _, container := range data.Kubernetes_objects[0].Containers {
						container_usage_metrics, err := json.Marshal(container.Metrics)
						if err != nil {
							log.Errorf("Unable to marshal container usage data: %v", err.Error())
							continue
						}

						workload_metric := model.WorkloadMetrics{
							OrgId:         rhAccount.OrgId,
							WorkloadID:    workload.ID,
							ContainerName: container.Container_name,
							IntervalStart: interval_start_time,
							IntervalEnd:   interval_end_time,
							UsageMetrics:  container_usage_metrics,
						}
						workload_metric_arr = append(workload_metric_arr, workload_metric)
					}
				}
				if err := model.BatchInsertWorkloadMetrics(workload_metric_arr, rhAccount.OrgId); err != nil {
					log.Errorf("unable to batch insert to workload_metrics table. %v", err.Error())
					if isTransientKafkaProcessingError(err) {
						return err, false
					}
					continue
				}
			}

			maxEndtimeFromReport := maxEndTime.UTC()
			messageData := types.RecommendationKafkaMsg{
				Request_id: kafkaMsg.Request_id,
				Metadata: types.RecommendationMetadata{
					Org_id:             kafkaMsg.Metadata.Org_id,
					Workload_id:        workload.ID,
					Max_endtime_report: maxEndtimeFromReport,
					Experiment_name:    experiment_name,
					ExperimentType:     types.PayloadTypeContainer,
				},
			}

			msgBytes, err := json.Marshal(messageData)
			if err != nil {
				log.Error("Error marshaling JSON:", err)
				continue
			}

			msgProduceErr := kafka_internal.SendMessage(msgBytes, appCfg.RecommendationTopic, experiment_name)
			if msgProduceErr != nil {
				log.Errorf("Failed to produce message: %v for experiment - %s and end_interval - %s\n", msgProduceErr.Error(), experiment_name, maxEndtimeFromReport)
			} else {
				log.Infof("Recommendation request sent for experiment - %s and end_interval - %s", experiment_name, maxEndtimeFromReport)
			}
		}

	case types.PayloadTypeNamespace:
		namespaceGroupMap := df.GroupBy("namespace").GetGroups()
		for _, v := range namespaceGroupMap {
			intervalEndTimeValues := v.Col("interval_end").Records()
			maxEndTime, err := utils.MaxIntervalEndTime(intervalEndTimeValues)
			if err != nil {
				log.Errorf("unable to convert string to time: %s", err)
				continue
			}

			namespaceRows := v.Maps()
			namespaceName := kruizePayload.AssertAndConvertToString(namespaceRows[0]["namespace"])

			experimentName := utils.GenerateNamespaceExperimentName(
				kafkaMsg.Metadata.Org_id,
				kafkaMsg.Metadata.Source_id,
				kafkaMsg.Metadata.Cluster_uuid,
				namespaceName,
			)

			clusterIdentifier := kafkaMsg.Metadata.Org_id + ";" + kafkaMsg.Metadata.Cluster_uuid
			experimentCreateError := kruize.CreateNamespaceExperiment(experimentName, clusterIdentifier, namespaceName)
			if experimentCreateError != nil {
				log.Error(experimentCreateError.Error())
				continue
			}

			workload := model.Workload{
				OrgId:           rhAccount.OrgId,
				ClusterID:       cluster.ID,
				ExperimentName:  experimentName,
				Namespace:       namespaceName,
				WorkloadType:    w.Namespace,
				MetricsUploadAt: maxEndTime,
			}
			if workloadCreateErr := workload.CreateWorkload(); workloadCreateErr != nil {
				log.Errorf("unable to save workload record: %v. Error: %v", workload, workloadCreateErr)
				if isTransientKafkaProcessingError(workloadCreateErr) {
					return workloadCreateErr, false
				}
				continue
			}

			var namespaceChunks [][]namespacePayload.UpdateNamespaceResult
			updateResultPayload := namespacePayload.GetUpdateNamespaceResultPayload(experimentName, namespaceRows)
			if len(updateResultPayload) > appCfg.KruizeMaxBulkChunkSize {
				namespaceChunks = SliceMetricsUpdatePayloadToChunks(updateResultPayload)
			} else {
				namespaceChunks = append(namespaceChunks, updateResultPayload)
			}

			for _, chunk := range namespaceChunks {
				_, err := kruize.UpdateNamespaceResults(experimentName, chunk)
				if err != nil {
					log.Error(err, experimentName)
					continue
				}

				workloadMetricSlice := []model.WorkloadMetrics{}
				for _, data := range chunk {
					interval_start_time, err := utils.ConvertISO8601StringToTime(data.IntervalStartTime)
					if err != nil {
						log.Errorf("Error for start time: %s", err)
						continue
					}
					interval_end_time, err := utils.ConvertISO8601StringToTime(data.IntervalEndTime)
					if err != nil {
						log.Errorf("Error for end time: %s", err)
						continue
					}

					namespaceMetrics := data.KubernetesObjects[0].Namespaces.Metrics
					namespaceUsageMetrics, err := json.Marshal(namespaceMetrics)
					if err != nil {
						log.Errorf("unable to marshal namespace usage data: %v", err)
						continue
					}

					workloadMetricNamespace := model.WorkloadMetrics{
						OrgId:         rhAccount.OrgId,
						WorkloadID:    workload.ID,
						NamespaceName: namespaceName,
						MetricType:    "namespace",
						IntervalStart: interval_start_time,
						IntervalEnd:   interval_end_time,
						UsageMetrics:  namespaceUsageMetrics,
					}
					workloadMetricSlice = append(workloadMetricSlice, workloadMetricNamespace)
				}

				if err := model.BatchInsertWorkloadMetrics(workloadMetricSlice, rhAccount.OrgId); err != nil {
					log.Errorf("unable to batch insert namespace metrics to workload_metrics table. Error: %v", err)
					if isTransientKafkaProcessingError(err) {
						return err, false
					}
					continue
				}
			}

			maxEndtimeFromReport := maxEndTime.UTC()
			messageData := types.RecommendationKafkaMsg{
				Request_id: kafkaMsg.Request_id,
				Metadata: types.RecommendationMetadata{
					Org_id:             kafkaMsg.Metadata.Org_id,
					Workload_id:        workload.ID,
					Max_endtime_report: maxEndtimeFromReport,
					Experiment_name:    experimentName,
					ExperimentType:     types.PayloadTypeNamespace,
				},
			}

			msgBytes, err := json.Marshal(messageData)
			if err != nil {
				log.Error("Error marshaling JSON:", err)
				continue
			}

			msgProduceErr := kafka_internal.SendMessage(msgBytes, appCfg.RecommendationTopic, experimentName)
			if msgProduceErr != nil {
				log.Errorf("failed to produce message: %v for experiment - %s and end_interval - %s\n", msgProduceErr.Error(), experimentName, maxEndtimeFromReport)
			} else {
				log.Infof("recommendation request sent for experiment - %s and end_interval - %s", experimentName, maxEndtimeFromReport)
			}
		}
	}

	return nil, false
}
