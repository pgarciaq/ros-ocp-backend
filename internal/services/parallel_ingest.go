package services

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/go-gota/gota/dataframe"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
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
	appCfg := config.GetConfig()
	workers := appCfg.ManifestDownloadWorkers
	if workers <= 0 {
		workers = 2
	}

	// For a single file, skip the goroutine overhead entirely.
	if len(kafkaMsg.Files) <= 1 {
		for fileIdx, file := range kafkaMsg.Files {
			tErr, pErr := processOneFile(ctx, pool, log, kafkaMsg, manifestID, useNativeCSVIngest, rhAccount, cluster, fileIdx, file)
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

	for fileIdx, file := range kafkaMsg.Files {
		fileIdx, file := fileIdx, file
		eg.Go(func() error {
			tErr, pErr := processOneFile(egCtx, pool, log, kafkaMsg, manifestID, useNativeCSVIngest, rhAccount, cluster, fileIdx, file)
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
		return processNativeFile(ctx, pool, log, kafkaMsg, manifestID, filename, file, rhAccount, cluster)
	}

	// Legacy Kruize fallback path.
	return processLegacyFile(ctx, pool, log, kafkaMsg, manifestID, filename, file, rhAccount, cluster)
}

func processNativeFile(
	ctx context.Context,
	pool *pgxpool.Pool,
	log *logrus.Entry,
	kafkaMsg types.KafkaMsg,
	manifestID, filename, file string,
	rhAccount *model.RHAccount,
	cluster *model.Cluster,
) (transientErr error, permanentFailed bool) {
	if plugin.EnabledFor("vm") && engine.IsClusterInstanceTypesFile(file) {
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

	case types.PayloadTypeVM, types.PayloadTypeVMGPU:
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
		}
	}

	return nil, false
}

func processLegacyFile(
	ctx context.Context,
	pool *pgxpool.Pool,
	log *logrus.Entry,
	kafkaMsg types.KafkaMsg,
	manifestID, filename, file string,
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
