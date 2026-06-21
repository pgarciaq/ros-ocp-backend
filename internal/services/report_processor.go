package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/go-gota/gota/dataframe"
	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	kafka_internal "github.com/redhatinsights/ros-ocp-backend/internal/kafka"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
	"github.com/redhatinsights/ros-ocp-backend/internal/types/kruizePayload"
	namespacePayload "github.com/redhatinsights/ros-ocp-backend/internal/types/kruizePayload/namespace"
	w "github.com/redhatinsights/ros-ocp-backend/internal/types/workload"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils/kruize"
)

var cfg *config.Config = config.GetConfig()

// ingestCSVFromURL downloads a report payload and runs native plugin ingest or a fallback handler.
func ingestCSVFromURL(
	ctx context.Context,
	fileURL, orgID, clusterUUID, csvType string,
	fallback func(context.Context, *pgxpool.Pool, io.Reader, string, string) error,
) error {
	var body io.ReadCloser
	if err := metrics.ObservePhase(metrics.PhaseDownload, func() error {
		var err error
		body, err = utils.ReadCSVBodyFromUrl(fileURL)
		return err
	}); err != nil {
		csvFetchError.Inc()
		return fmt.Errorf("fetch %s CSV: %w", csvType, err)
	}
	defer body.Close()

	pool := db.GetPool()
	return metrics.ObservePhase(metrics.PhaseParseDigest, func() error {
		handled, err := nativeCSVIngestViaPlugins(ctx, pool, body, orgID, clusterUUID, csvType)
		if err != nil {
			return err
		}
		if !handled && fallback != nil {
			return fallback(ctx, pool, body, orgID, clusterUUID)
		}
		return nil
	})
}

// nativeCSVIngestViaPlugins delegates to [plugin.DispatchCSV] which finds the
// enabled CSVIngestor for csvType, runs IngestCSV, then fires matching IngestHooks.
// Returns handled=false when no ingestor claimed csvType (caller should use the
// legacy fallback path).
func nativeCSVIngestViaPlugins(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID, csvType string) (handled bool, err error) {
	handled, _, hookErrs, err := plugin.DispatchCSV(ctx, pool, r, orgID, clusterUUID, csvType)
	if err != nil {
		return handled, err
	}
	log := logging.GetLogger()
	for _, he := range hookErrs {
		log.Warnf("IngestHook %s failed (non-fatal): org=%s cluster=%s %v", he.HookName, orgID, clusterUUID, he.Err)
		PluginHookErrors.WithLabelValues(he.HookName, "after_ingest").Inc()
	}
	if len(hookErrs) > 0 {
		if setErr := engine.SetClusterIngestHooksFailed(ctx, pool, orgID, clusterUUID, true); setErr != nil {
			log.Warnf("unable to mark ingest_hooks_failed: %v", setErr)
		}
	} else if handled {
		if setErr := engine.SetClusterIngestHooksFailed(ctx, pool, orgID, clusterUUID, false); setErr != nil {
			log.Warnf("unable to clear ingest_hooks_failed: %v", setErr)
		}
	}
	return handled, nil
}

// processContainerDigestFallback mirrors container CSV ingestion when no CSVIngestor handles "container":
// parse digests, then upsert GPU/node digest tables only for plugins enabled in ROS_ENABLED_PLUGINS / defaults.
func processContainerDigestFallback(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	_, err := ingestion.ParseAndDigestCSV(ctx, pool, r, orgID, clusterUUID, ingestion.ParseDigestOptions{
		EnableGPU:  plugin.EnabledFor("gpu"),
		EnableNode: plugin.EnabledFor("node"),
	})
	return err
}

func ProcessReport(ctx context.Context, msg *kafka.Message, consumer *kafka.Consumer) {
	log := logging.GetLogger()
	cfg := config.GetConfig()
	validate := validator.New()

	pipelineStart := time.Now()
	pipelineSucceeded := false
	defer func() {
		status := "error"
		if pipelineSucceeded {
			status = "success"
		}
		metrics.ObservePipelineTotal(status, pipelineStart)
	}()

	commitOnPermanentFailure := func(reason string) {
		logPoisonMessage(log, msg, reason, nil)
		if consumer != nil {
			if err := kafka_internal.CommitMessage(consumer, msg); err != nil {
				log.Errorf("unable to commit poison message: %v", err)
			}
		}
	}

	var kafkaMsg types.KafkaMsg
	if !json.Valid([]byte(msg.Value)) {
		log.Errorf("Received message on kafka topic is not valid JSON (len=%d, partition=%s)", len(msg.Value), msg.TopicPartition)
		commitOnPermanentFailure("invalid JSON")
		return
	}
	if err := json.Unmarshal(msg.Value, &kafkaMsg); err != nil {
		log.Errorf("Unable to decode kafka message (len=%d, partition=%s): %v", len(msg.Value), msg.TopicPartition, err)
		commitOnPermanentFailure("unmarshal failed")
		return
	}
	if err := validate.Struct(kafkaMsg); err != nil {
		log.Errorf("Invalid kafka message: %s", err)
		commitOnPermanentFailure("validation failed")
		return
	}
	log = logging.Set_request_details(kafkaMsg)

	var kafkaTransientErr error
	var reportProcessingFailed bool
	recordKafkaTransient := func(err error) {
		if err != nil && isTransientKafkaProcessingError(err) {
			kafkaTransientErr = err
		}
	}
	if err := ctx.Err(); err != nil {
		log.Warnf("ProcessReport interrupted during shutdown: %v", err)
		recordKafkaTransient(err)
		return
	}

	// Create RHAccount and Cluster once before the file loop so both native
	// and legacy paths have valid rows for JOINs in the API read path.
	rhAccount := model.RHAccount{
		Account: kafkaMsg.Metadata.Account,
		OrgId:   kafkaMsg.Metadata.Org_id,
	}
	if err := rhAccount.CreateRHAccount(); err != nil {
		log.Errorf("unable to get or add record to rh_accounts table: %v. Error: %v", rhAccount, err)
		recordKafkaTransient(err)
		return
	}

	cluster := model.Cluster{
		TenantID:       rhAccount.ID,
		SourceId:       kafkaMsg.Metadata.Source_id,
		ClusterUUID:    kafkaMsg.Metadata.Cluster_uuid,
		ClusterAlias:   kafkaMsg.Metadata.Cluster_alias,
		LastReportedAt: time.Now().UTC(),
	}
	if err := cluster.CreateCluster(); err != nil {
		log.Errorf("unable to get or add record to clusters table: %v. Error: %v", cluster, err)
		recordKafkaTransient(err)
		return
	}

	var csvType types.PayloadType

	useNativeCSVIngest := !plugin.EnabledFor(plugin.KruizePluginName)

	pool := db.GetPool()
	manifestID := resolveManifestID(&kafkaMsg, log)
	if err := ensureManifestExpectations(ctx, pool, kafkaMsg); err != nil {
		log.Errorf("unable to register expected manifest files: %v", err)
		recordKafkaTransient(err)
		return
	}

	for fileIdx, file := range kafkaMsg.Files {
		if err := ctx.Err(); err != nil {
			log.Warnf("ProcessReport interrupted during shutdown before file %d: %v", fileIdx, err)
			recordKafkaTransient(err)
			return
		}
		filename := filenameForFileIndex(kafkaMsg, file, fileIdx)
		if shouldSkipProcessedFile(ctx, pool, manifestID, filename) {
			log.Infof("skipping already-processed file %s for manifest %s", filename, manifestID)
			continue
		}
		if useNativeCSVIngest && plugin.EnabledFor("vm") && engine.IsClusterInstanceTypesFile(file) {
			reportType := "cluster_instance_types"
			markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
			if err := processClusterInstanceTypesIngest(ctx, file, kafkaMsg); err != nil {
				if isTransientKafkaProcessingError(err) {
					recordKafkaTransient(err)
					return
				}
				handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
				continue
			}
			markFileDone(ctx, pool, log, manifestID, filename)
			continue
		}

		csvType = utils.DetermineCSVType(file)
		reportType := string(csvType)
		if csvType == types.PayloadTypeUnknown {
			log.Infof("skipping unrecognized file %s (type %s)", filename, reportType)
			markFileDone(ctx, pool, log, manifestID, filename)
			continue
		}
		if useNativeCSVIngest && csvType == types.PayloadTypeContainer {
			markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
			if err := processContainerCSVIngest(ctx, file, kafkaMsg); err != nil {
				if isTransientKafkaProcessingError(err) {
					recordKafkaTransient(err)
					return
				}
				handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
				continue
			}
			markFileDone(ctx, pool, log, manifestID, filename)
			continue
		}
		if useNativeCSVIngest && csvType == types.PayloadTypeNamespace {
			markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
			if err := processNamespaceCSVIngest(ctx, file, kafkaMsg); err != nil {
				if isTransientKafkaProcessingError(err) {
					recordKafkaTransient(err)
					return
				}
				handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
				continue
			}
			markFileDone(ctx, pool, log, manifestID, filename)
			continue
		}
		if useNativeCSVIngest && csvType == types.PayloadTypeStorage {
			markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
			if err := processStorageCSVIngest(ctx, file, kafkaMsg); err != nil {
				if isTransientKafkaProcessingError(err) {
					recordKafkaTransient(err)
					return
				}
				handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
				continue
			}
			markFileDone(ctx, pool, log, manifestID, filename)
			continue
		}
		if useNativeCSVIngest && csvType == types.PayloadTypeSnapshot {
			markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
			if err := processSnapshotCSVIngest(ctx, file, kafkaMsg); err != nil {
				if isTransientKafkaProcessingError(err) {
					recordKafkaTransient(err)
					return
				}
				handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
				continue
			}
			markFileDone(ctx, pool, log, manifestID, filename)
			continue
		}
		if useNativeCSVIngest && csvType == types.PayloadTypeClusterQuota {
			markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
			if err := processClusterQuotaCSVIngest(ctx, file, kafkaMsg); err != nil {
				if isTransientKafkaProcessingError(err) {
					recordKafkaTransient(err)
					return
				}
				handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
				continue
			}
			markFileDone(ctx, pool, log, manifestID, filename)
			continue
		}
		if useNativeCSVIngest && (csvType == types.PayloadTypeVM || csvType == types.PayloadTypeVMGPU) {
			if plugin.EnabledFor("vm") {
				markFileProcessing(ctx, pool, log, kafkaMsg, filename, reportType)
				if err := processVMCsvIngest(ctx, file, kafkaMsg, csvType); err != nil {
					if isTransientKafkaProcessingError(err) {
						recordKafkaTransient(err)
						return
					}
					handlePermanentFileError(ctx, pool, log, kafkaMsg, filename, reportType, err)
					continue
				}
				markFileDone(ctx, pool, log, manifestID, filename)
			}
			continue
		}

		data, fetchError := utils.ReadCSVFromUrl(file)
		if fetchError != nil {
			reportProcessingFailed = true
			csvFetchError.Inc()
			log.Errorf("unable to read CSV from URL: %s", fetchError.Error())
			recordKafkaTransient(fetchError)
			continue
		}
		columnHeaders := types.GetColumnMapping(csvType)
		df := dataframe.LoadRecords(
			data,
			dataframe.WithTypes(columnHeaders),
		)
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
			continue
		}

		switch csvType {
		case types.PayloadTypeContainer:
			// grouping container(row in csv) by deployment.
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

				// Create workload entry into the table.
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
					recordKafkaTransient(err)
					continue
				}

				var k8s_object_chunks [][]kruizePayload.UpdateResult
				update_result_payload_data := kruizePayload.GetUpdateResultPayload(experiment_name, k8s_object)
				if len(update_result_payload_data) > cfg.KruizeMaxBulkChunkSize {
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
						recordKafkaTransient(err)
						continue
					}
				}

				// sending kafka msg to poller for recommendation request
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

				msgProduceErr := kafka_internal.SendMessage(msgBytes, cfg.RecommendationTopic, experiment_name)
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
					recordKafkaTransient(workloadCreateErr)
					continue
				}

				var namespaceChunks [][]namespacePayload.UpdateNamespaceResult
				updateResultPayload := namespacePayload.GetUpdateNamespaceResultPayload(experimentName, namespaceRows)
				if len(updateResultPayload) > cfg.KruizeMaxBulkChunkSize {
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
						recordKafkaTransient(err)
						continue
					}
				}

				// sending kafka msg to poller for recommendation request
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

				msgProduceErr := kafka_internal.SendMessage(msgBytes, cfg.RecommendationTopic, experimentName)
				if msgProduceErr != nil {
					log.Errorf("failed to produce message: %v for experiment - %s and end_interval - %s\n", msgProduceErr.Error(), experimentName, maxEndtimeFromReport)
				} else {
					log.Infof("recommendation request sent for experiment - %s and end_interval - %s", experimentName, maxEndtimeFromReport)
				}
			}

		}

	}

	if useNativeCSVIngest {
		if recErr := scheduleManifestRecommendations(ctx, pool, kafkaMsg); recErr != nil {
			log.Errorf("deferred manifest recommendations failed: %v", recErr)
			if isTransientKafkaProcessingError(recErr) {
				recordKafkaTransient(recErr)
			}
		}
	}

	appCfg := config.GetConfig()
	if consumer != nil && !appCfg.KafkaAutoCommit {
		if kafkaTransientErr != nil {
			handleKafkaTransientError(consumer, kafkaProducerForRetry(), msg, kafkaTransientErr)
		} else if err := kafka_internal.CommitMessage(consumer, msg); err != nil {
			log.Errorf("kafka: unable to commit offset after successful processing (%s): %v", msg.TopicPartition, err)
		} else {
			metrics.KafkaMessagesProcessed.Inc()
			pipelineSucceeded = true
		}
	} else if appCfg.KafkaAutoCommit && kafkaTransientErr == nil && !reportProcessingFailed {
		metrics.KafkaMessagesProcessed.Inc()
		pipelineSucceeded = true
	}
}

// processContainerCSVNative runs ingest and recommendations (used by unit tests).
func processContainerCSVNative(fileURL string, kafkaMsg types.KafkaMsg) error {
	if err := processContainerCSVIngest(context.Background(), fileURL, kafkaMsg); err != nil {
		return err
	}
	return runContainerRecommendations(kafkaMsg)
}

// processContainerCSVIngest downloads a container CSV and upserts daily digests.
func processContainerCSVIngest(ctx context.Context, fileURL string, kafkaMsg types.KafkaMsg) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	err := ingestCSVFromURL(ctx, fileURL, orgID, clusterUUID, "container", func(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
		return processContainerDigestFallback(ctx, pool, r, orgID, clusterUUID)
	})
	if err != nil {
		log.Errorf("native engine: digest processing failed: %v", err)
		ingestionErrors.WithLabelValues("digest").Inc()
		return fmt.Errorf("digest processing: %w", err)
	}
	return nil
}

// runContainerRecommendations computes and persists container recommendations for a cluster.
func runContainerRecommendations(kafkaMsg types.KafkaMsg) error {
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	ctx := context.Background()
	pool := db.GetPool()

	now := time.Now().UTC()
	appCfg := config.GetConfig()
	start := now.AddDate(0, 0, -appCfg.MaxLookbackDays)
	oomCfg := engine.OOMConfig{
		BaseBump: appCfg.OOMBaseBump,
		MaxBump:  appCfg.OOMMaxBump,
	}

	var costData *costdata.ClusterCostData
	if appCfg.SavingsEstimatesEnabled {
		costProvider := getCostDataProvider(appCfg)
		var costErr error
		costData, costErr = costProvider.GetEffectiveRates(ctx, orgID, clusterUUID, start, now)
		if costErr != nil {
			log.Warnf("native engine: cost data fetch failed (NotifNoCostData applied via ApplySavingsEstimates): %v", costErr)
			costData = nil
		}
	}

	oldRecs, err := engine.ReadClusterOldRecommendationsByEngine(ctx, pool, orgID, clusterUUID)
	if err != nil {
		log.Errorf("native engine: reading old recommendations failed: %v", err)
		oldRecs = nil
	}

	engine.EnsureRecommendationPartitionsAtStartup(ctx, pool)

	batchState := &engine.ContainerRecBatchState{}
	totalWritten := 0
	strictAnalytics := appCfg.IngestStrictAnalytics

	cycleStart := time.Now().UTC()
	tRec := cycleStart
	err = engine.RecommendWorkloadsStreaming(ctx, pool, orgID, clusterUUID, start, now, oomCfg, func(batch []engine.ContainerRec) error {
		var n int
		writeErr := metrics.ObservePhase(metrics.PhaseWriteRecommendations, func() error {
			var batchErr error
			n, batchErr = engine.WriteContainerRecBatch(
				ctx, pool, log, batch, oldRecs, costData, orgID, clusterUUID, strictAnalytics, batchState,
				func() { ingestionErrors.WithLabelValues("write").Inc() },
			)
			return batchErr
		})
		if writeErr != nil {
			return writeErr
		}
		totalWritten += n
		return nil
	})
	metrics.ObservePipelinePhase(metrics.PhaseRecommend, tRec)
	metrics.ObserveRecommendation("container", tRec)

	if err != nil {
		log.Errorf("native engine: recommendation failed: %v", err)
		ingestionErrors.WithLabelValues("recommend").Inc()
		return fmt.Errorf("recommend workloads: %w", err)
	}

	if totalWritten == 0 {
		log.Info("native engine: no recommendations produced")
		return nil
	}
	metrics.IncRecommendationsWritten("container", totalWritten)
	log.Infof("native engine: wrote %d recommendations", totalWritten)

	markedStale, staleErr := engine.MarkUnreportedContainersStale(ctx, pool, orgID, clusterUUID, cycleStart)
	if staleErr != nil {
		log.Warnf("native engine: marking unreported containers stale failed: %v", staleErr)
	} else if markedStale > 0 {
		log.Infof("native engine: marked %d unreported container recommendations stale", markedStale)
	}

	if refreshErr := metrics.ObservePhase(metrics.PhaseMetadataRefresh, func() error {
		return engine.RefreshOrgMetadata(ctx, pool, orgID)
	}); refreshErr != nil {
		log.Errorf("native engine: org metadata refresh failed: %v", refreshErr)
		return fmt.Errorf("refresh org metadata: %w", refreshErr)
	}

	if oldRecs == nil {
		log.Warn("native engine: skipping quality metrics (old recs unavailable)")
		batchState.Degraded = true
	}

	if batchState.Degraded {
		log.WithFields(map[string]interface{}{
			"org_id": orgID, "cluster_uuid": clusterUUID, "strict_analytics": strictAnalytics,
		}).Warn("native engine: analytics pipeline incomplete (history and/or quality) — container recommendations were written")
		if markErr := engine.SetClusterAnalyticsIncomplete(ctx, pool, orgID, clusterUUID, true); markErr != nil {
			log.Warnf("native engine: failed to mark cluster analytics incomplete: %v", markErr)
		}
	} else if totalWritten > 0 {
		if markErr := engine.SetClusterAnalyticsIncomplete(ctx, pool, orgID, clusterUUID, false); markErr != nil {
			log.Warnf("native engine: failed to clear cluster analytics incomplete flag: %v", markErr)
		}
	}

	tPost := time.Now()
	eg, egCtx := errgroup.WithContext(ctx)

	if plugin.EnabledFor("gpu") {
		eg.Go(func() error {
			if err := engine.MarkContainersWithGPU(egCtx, pool, orgID, clusterUUID); err != nil {
				log.Warnf("native engine: marking GPU containers failed: %v", err)
			}
			gpuTerms, termErr := engine.LoadTermConfigCached(egCtx, pool, orgID, "gpu")
			if termErr != nil {
				log.Warnf("native engine: load term config for GPU classification failed: %v", termErr)
				gpuTerms = engine.DefaultTermsForPlugin("gpu")
			}
			if err := engine.StoreGPUClassifications(egCtx, pool, orgID, clusterUUID, gpuTerms, costData); err != nil {
				log.Warnf("native engine: storing GPU classifications failed: %v", err)
			}
			if err := engine.ComputeAndPersistNodeGPUTimeSlicingRecs(egCtx, pool, orgID, clusterUUID, gpuTerms, costData); err != nil {
				log.Warnf("native engine: persist node GPU time-slicing failed: %v", err)
			}
			return nil
		})
	}

	eg.Go(func() error {
		return runNodeRecommendations(egCtx, pool, orgID, clusterUUID, start, now, appCfg, costData)
	})

	if err := eg.Wait(); err != nil {
		log.Warnf("native engine: post-container recommendations incomplete: %v", err)
		return fmt.Errorf("post-container recommendations: %w", err)
	}

	if plugin.EnabledFor("quota") {
		if quotaErr := engine.RunQuotaRecommendations(ctx, pool, orgID, clusterUUID); quotaErr != nil {
			log.Warnf("native engine: quota recommendations failed: %v", quotaErr)
			PluginHookErrors.WithLabelValues("quota", "after_container_recs").Inc()
		}
	}
	if plugin.EnabledFor("cluster-quota") {
		if crqErr := engine.RunClusterQuotaRecommendations(ctx, pool, orgID, clusterUUID); crqErr != nil {
			log.Warnf("native engine: cluster-quota recommendations failed: %v", crqErr)
			PluginHookErrors.WithLabelValues("cluster-quota", "after_container_recs").Inc()
		}
	}
	metrics.ObservePipelinePhase(metrics.PhasePostProcess, tPost)
	return nil
}

func processNamespaceCSVNative(fileURL string, kafkaMsg types.KafkaMsg) error {
	if err := processNamespaceCSVIngest(context.Background(), fileURL, kafkaMsg); err != nil {
		return err
	}
	return runNamespaceRecommendations(kafkaMsg)
}

// runNodeRecommendations queries daily_node_digests for the cluster, computes
// Tier 1 node utilization signals, and persists the results.
func runNodeRecommendations(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, start, end time.Time, appCfg *config.Config, costData *costdata.ClusterCostData) error {
	t0 := time.Now()
	defer func() {
		metrics.ObservePipelinePhase(metrics.PhaseRecommend, t0)
		metrics.ObserveRecommendation("node", t0)
	}()

	log := logging.ForOrg(orgID, clusterUUID)

	digests, err := engine.QueryNodeDigests(ctx, pool, orgID, clusterUUID, start, end)
	if err != nil {
		log.Warnf("node recs: query digests failed: %v", err)
		return fmt.Errorf("query node digests: %w", err)
	}
	if len(digests) == 0 {
		log.Info("node recs: no node digests")
		return nil
	}

	terms, err := engine.LoadTermConfigCached(ctx, pool, orgID, "node")
	if err != nil {
		log.Errorf("node recs: load term config failed, using defaults: %v", err)
		terms = engine.DefaultTermsForPlugin("node")
	}

	nodeSettings, err := engine.ResolveNodeThresholdSettings(ctx, pool, orgID)
	if err != nil {
		log.Warnf("node recs: load threshold settings failed, using defaults: %v", err)
		nodeSettings = engine.DefaultNodeThresholdSettings()
	}

	cfg := engine.NodeRecConfigFromThresholds(nodeSettings)
	recs := engine.RecommendNodes(digests, cfg, nodeSettings, terms)
	if len(recs) == 0 {
		log.Info("node recs: no recommendations produced")
		return nil
	}

	engine.ApplyNodeSavings(recs, costData)

	validTerms := make([]string, len(terms))
	for i, tc := range terms {
		validTerms[i] = tc.Name
	}
	if err := metrics.ObservePhase(metrics.PhaseWriteRecommendations, func() error {
		return engine.PersistNodeRecommendations(ctx, pool, orgID, clusterUUID, recs, validTerms)
	}); err != nil {
		log.Errorf("node recs: persist failed: %v", err)
		return fmt.Errorf("persist node recommendations: %w", err)
	}
	metrics.IncRecommendationsWritten("node", len(recs))
	return nil
}

func processNamespaceCSVIngest(ctx context.Context, fileURL string, kafkaMsg types.KafkaMsg) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	err := ingestCSVFromURL(ctx, fileURL, orgID, clusterUUID, "namespace", func(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
		return ingestion.ProcessNamespaceCSVToDigests(ctx, pool, r, orgID, clusterUUID)
	})
	if err != nil {
		log.Errorf("native namespace engine: digest processing failed: %v", err)
		return fmt.Errorf("namespace digest processing: %w", err)
	}
	return nil
}

func runNamespaceRecommendations(kafkaMsg types.KafkaMsg) error {
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	ctx := context.Background()
	pool := db.GetPool()

	now := time.Now().UTC()
	nsCfg := config.GetConfig()
	start := now.AddDate(0, 0, -nsCfg.MaxLookbackDays)
	tNs := time.Now()
	results, err := engine.RecommendAllNamespaces(ctx, pool, orgID, clusterUUID, start, now)
	metrics.ObservePipelinePhase(metrics.PhaseRecommend, tNs)
	metrics.ObserveRecommendation("namespace", tNs)
	if err != nil {
		log.Errorf("native namespace engine: recommendation failed: %v", err)
		return fmt.Errorf("recommend namespaces: %w", err)
	}

	if len(results) == 0 {
		log.Info("native namespace engine: no recommendations produced")
		return nil
	}

	if nsCfg.SavingsEstimatesEnabled {
		costProvider := getCostDataProvider(nsCfg)
		costData, costErr := costProvider.GetEffectiveRates(ctx, orgID, clusterUUID, start, now)
		if costErr != nil {
			log.Warnf("native namespace engine: cost data fetch failed (NotifNoCostData applied): %v", costErr)
			costData = nil
		}
		engine.ApplyNamespaceSavingsEstimates(results, costData)
	}

	if err := metrics.ObservePhase(metrics.PhaseWriteRecommendations, func() error {
		return engine.WriteNamespaceRecommendations(ctx, pool, results)
	}); err != nil {
		log.Errorf("native namespace engine: writing recommendations failed: %v", err)
		return fmt.Errorf("write namespace recommendations: %w", err)
	}

	bhWritten := 0
	var bhResults []engine.NamespaceRec
	bhResults, bhErr := engine.RecommendBusinessHoursNamespaces(ctx, pool, orgID, clusterUUID, start, now)
	if bhErr != nil {
		log.Errorf("native namespace engine: business hours recommendation failed: %v", bhErr)
		return fmt.Errorf("recommend business hours namespaces: %w", bhErr)
	} else if len(bhResults) > 0 {
		if err := metrics.ObservePhase(metrics.PhaseWriteRecommendations, func() error {
			return engine.WriteNamespaceRecommendations(ctx, pool, bhResults)
		}); err != nil {
			log.Errorf("native namespace engine: writing business hours recommendations failed: %v", err)
			return fmt.Errorf("write business hours namespace recommendations: %w", err)
		}
		bhWritten = len(bhResults)
	}

	tPost := time.Now()
	if err := engine.AggregateNamespaceIdleState(ctx, pool, orgID, clusterUUID); err != nil {
		log.Errorf("native namespace engine: idle state aggregation failed: %v", err)
		return fmt.Errorf("aggregate namespace idle state: %w", err)
	}
	metrics.IncRecommendationsWritten("namespace", len(results)+bhWritten)
	log.Infof("native namespace engine: wrote %d all-hours and %d business-hours recommendations", len(results), bhWritten)

	var bhHistory []engine.NamespaceRec
	if bhWritten > 0 {
		bhHistory = bhResults
	}
	namespaceAnalyticsDegraded, histErr := engine.WriteNamespaceRecommendationHistories(
		ctx, pool, results, bhHistory, isTransientKafkaProcessingError,
	)
	if histErr != nil {
		log.Errorf("native namespace engine: writing history failed: %v", histErr)
		return fmt.Errorf("write namespace history: %w", histErr)
	}
	if namespaceAnalyticsDegraded {
		log.Warn("native namespace engine: analytics pipeline incomplete (history) — namespace recommendations were written")
	}

	if plugin.EnabledFor("quota") {
		if quotaErr := engine.RunQuotaRecommendations(ctx, pool, orgID, clusterUUID); quotaErr != nil {
			log.Warnf("native namespace engine: quota recommendations failed: %v", quotaErr)
			PluginHookErrors.WithLabelValues("quota", "after_namespace_ingest").Inc()
		}
	}
	metrics.ObservePipelinePhase(metrics.PhasePostProcess, tPost)
	return nil
}

func processClusterQuotaCSVNative(fileURL string, kafkaMsg types.KafkaMsg) error {
	if err := processClusterQuotaCSVIngest(context.Background(), fileURL, kafkaMsg); err != nil {
		return err
	}
	return runClusterQuotaRecommendations(kafkaMsg)
}

func processClusterQuotaCSVIngest(ctx context.Context, fileURL string, kafkaMsg types.KafkaMsg) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	err := ingestCSVFromURL(ctx, fileURL, orgID, clusterUUID, string(types.PayloadTypeClusterQuota), func(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
		return ingestion.ProcessClusterQuotaCSV(ctx, pool, r, orgID, clusterUUID)
	})
	if err != nil {
		log.Errorf("native cluster-quota engine: ingest failed: %v", err)
		return fmt.Errorf("cluster-quota ingest: %w", err)
	}
	return nil
}

func processClusterInstanceTypesIngest(ctx context.Context, fileURL string, kafkaMsg types.KafkaMsg) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	var body io.ReadCloser
	if err := metrics.ObservePhase(metrics.PhaseDownload, func() error {
		var err error
		body, err = utils.ReadCSVBodyFromUrl(fileURL)
		return err
	}); err != nil {
		csvFetchError.Inc()
		log.Errorf("native VM engine: unable to fetch cluster instance types JSON: %v", err)
		return fmt.Errorf("fetch cluster instance types JSON: %w", err)
	}
	defer body.Close()

	pool := db.GetPool()
	if err := metrics.ObservePhase(metrics.PhaseParseDigest, func() error {
		return engine.IngestClusterInstanceTypesFromReader(ctx, pool, body, orgID, clusterUUID)
	}); err != nil {
		log.Errorf("native VM engine: cluster instance types ingest failed: %v", err)
		return fmt.Errorf("cluster instance types ingest: %w", err)
	}
	log.Info("native VM engine: cluster instance types ingested")
	return nil
}

// processVMCsvNative runs ingest only (used by unit tests). VM recommendations are
// deferred to runManifestRecommendations after all manifest files complete.
func processVMCsvNative(fileURL string, kafkaMsg types.KafkaMsg, csvType types.PayloadType) error {
	return processVMCsvIngest(context.Background(), fileURL, kafkaMsg, csvType)
}

func processVMCsvIngest(ctx context.Context, fileURL string, kafkaMsg types.KafkaMsg, csvType types.PayloadType) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	err := ingestCSVFromURL(ctx, fileURL, orgID, clusterUUID, string(csvType), func(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
		return ingestion.ProcessVMCSV(ctx, pool, r, orgID, clusterUUID)
	})
	if err != nil {
		log.Errorf("native VM engine: ingest failed: %v", err)
		return fmt.Errorf("VM ingest: %w", err)
	}
	return nil
}

func processStorageCSVNative(fileURL string, kafkaMsg types.KafkaMsg) error {
	if err := processStorageCSVIngest(context.Background(), fileURL, kafkaMsg); err != nil {
		return err
	}
	return runStorageRecommendations(kafkaMsg)
}

func processStorageCSVIngest(ctx context.Context, fileURL string, kafkaMsg types.KafkaMsg) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	err := ingestCSVFromURL(ctx, fileURL, orgID, clusterUUID, string(types.PayloadTypeStorage), func(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
		return ingestion.ProcessStorageCSV(ctx, pool, r, orgID, clusterUUID)
	})
	if err != nil {
		log.Errorf("native storage engine: digest processing failed: %v", err)
		return fmt.Errorf("storage digest processing: %w", err)
	}
	return nil
}

func runStorageRecommendations(kafkaMsg types.KafkaMsg) error {
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	ctx := context.Background()
	pool := db.GetPool()

	terms, err := engine.LoadTermConfigCached(ctx, pool, orgID, "pvc")
	if err != nil {
		log.Errorf("native storage engine: load term config failed, using defaults: %v", err)
		terms = engine.DefaultTermsForPlugin("pvc")
	}

	tPVC := time.Now()
	results, err := engine.RecommendPVCs(ctx, pool, orgID, clusterUUID, terms)
	metrics.ObservePipelinePhase(metrics.PhaseRecommend, tPVC)
	metrics.ObserveRecommendation("pvc", tPVC)
	if err != nil {
		log.Errorf("native storage engine: PVC recommendation failed: %v", err)
		return fmt.Errorf("recommend PVCs: %w", err)
	}

	if len(results) == 0 {
		log.Info("native storage engine: no PVC recommendations")
		return nil
	}

	appCfg := config.GetConfig()
	var costData *costdata.ClusterCostData
	if appCfg.SavingsEstimatesEnabled {
		now := time.Now().UTC()
		start := now.AddDate(0, 0, -appCfg.MaxLookbackDays)
		costProvider := getCostDataProvider(appCfg)
		var costErr error
		costData, costErr = costProvider.GetEffectiveRates(ctx, orgID, clusterUUID, start, now)
		if costErr != nil {
			log.Warnf("native storage engine: cost data fetch failed (NotifNoCostData applied via ApplyPVCSavings): %v", costErr)
			costData = nil
		}
	}
	engine.ApplyPVCSavings(results, costData)

	validTerms := make([]string, len(terms))
	for i, tc := range terms {
		validTerms[i] = tc.Name
	}
	if err := metrics.ObservePhase(metrics.PhaseWriteRecommendations, func() error {
		return engine.WritePVCRecommendations(ctx, pool, results, validTerms)
	}); err != nil {
		log.Errorf("native storage engine: writing PVC recommendations failed: %v", err)
		return fmt.Errorf("write PVC recommendations: %w", err)
	}
	metrics.IncRecommendationsWritten("pvc", len(results))
	log.Infof("native storage engine: wrote %d PVC recommendations", len(results))
	return nil
}

func processSnapshotCSVNative(fileURL string, kafkaMsg types.KafkaMsg) error {
	if err := processSnapshotCSVIngest(context.Background(), fileURL, kafkaMsg); err != nil {
		return err
	}
	return runSnapshotRecommendations(kafkaMsg)
}

func processSnapshotCSVIngest(ctx context.Context, fileURL string, kafkaMsg types.KafkaMsg) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	err := ingestCSVFromURL(ctx, fileURL, orgID, clusterUUID, string(types.PayloadTypeSnapshot), func(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
		return ingestion.ProcessSnapshotCSV(ctx, pool, r, orgID, clusterUUID)
	})
	if err != nil {
		log.Errorf("native snapshot engine: ingestion failed: %v", err)
		return fmt.Errorf("snapshot ingestion: %w", err)
	}
	return nil
}

func runSnapshotRecommendations(kafkaMsg types.KafkaMsg) error {
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	ctx := context.Background()
	pool := db.GetPool()

	appCfg := config.GetConfig()
	var costData *costdata.ClusterCostData
	if appCfg.SavingsEstimatesEnabled {
		now := time.Now().UTC()
		start := now.AddDate(0, 0, -appCfg.MaxLookbackDays)
		costProvider := getCostDataProvider(appCfg)
		var costErr error
		costData, costErr = costProvider.GetEffectiveRates(ctx, orgID, clusterUUID, start, now)
		if costErr != nil {
			log.Warnf("native snapshot engine: cost data fetch failed (using static snapshot cost rate): %v", costErr)
			costData = nil
		}
	}

	settings, err := engine.ResolveSnapshotSettings(ctx, pool, orgID, costData)
	if err != nil {
		log.Errorf("native snapshot engine: settings resolution failed: %v", err)
		return fmt.Errorf("snapshot settings: %w", err)
	}

	tSnap := time.Now()
	recs, err := engine.ClassifySnapshots(ctx, pool, orgID, clusterUUID, settings)
	metrics.ObservePipelinePhase(metrics.PhaseRecommend, tSnap)
	metrics.ObserveRecommendation("snapshot", tSnap)
	if err != nil {
		log.Errorf("native snapshot engine: classification failed: %v", err)
		return fmt.Errorf("classify snapshots: %w", err)
	}

	if len(recs) > 0 {
		if err := metrics.ObservePhase(metrics.PhaseWriteRecommendations, func() error {
			return engine.WriteSnapshotRecommendations(ctx, pool, recs)
		}); err != nil {
			log.Errorf("native snapshot engine: writing recommendations failed: %v", err)
			return fmt.Errorf("write snapshot recommendations: %w", err)
		}
		log.Infof("native snapshot engine: wrote %d snapshot recommendations", len(recs))
	}

	var removed int64
	if err := metrics.ObservePhase(metrics.PhaseWriteRecommendations, func() error {
		var reconcileErr error
		removed, reconcileErr = engine.ReconcileSnapshotRecommendations(ctx, pool, orgID, clusterUUID, settings.InventoryFreshHours, appCfg.SnapshotStaleGraceHours)
		return reconcileErr
	}); err != nil {
		log.Errorf("native snapshot engine: reconciliation failed: %v", err)
		return fmt.Errorf("reconcile snapshots: %w", err)
	}
	if removed > 0 {
		log.Infof("native snapshot engine: reconciled (removed) %d stale recommendations", removed)
	}
	return nil
}

// getCostDataProvider returns a CostDataProvider based on configuration.
// Returns a NilCostDataProvider if savings estimates are disabled or KOKU_MASU_URL is not configured.
func getCostDataProvider(cfg *config.Config) costdata.CostDataProvider {
	if !cfg.SavingsEstimatesEnabled || cfg.KokuMasuURL == "" {
		return &costdata.NilCostDataProvider{}
	}
	timeout := time.Duration(cfg.GlobalHTTPClientTimeoutSecs) * time.Second
	return costdata.NewHTTPCostDataProvider(cfg.KokuMasuURL, timeout)
}
