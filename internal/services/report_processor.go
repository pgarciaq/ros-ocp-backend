package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

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
		log.Warnf("IngestHook %s failed (non-fatal): %v", he.HookName, he.Err)
		PluginHookErrors.WithLabelValues(he.HookName, "after_ingest").Inc()
	}
	return handled, nil
}

// processContainerDigestFallback mirrors container CSV ingestion when no CSVIngestor handles "container":
// parse digests, then upsert GPU/node digest tables only for plugins enabled in ROS_ENABLED_PLUGINS / defaults.
func processContainerDigestFallback(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ingestion.ParseAndDigestCSV(ctx, pool, r, orgID, clusterUUID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if plugin.EnabledFor("gpu") {
		if err := ingestion.UpsertGPUDigests(ctx, pool, rows, orgID, clusterUUID); err != nil {
			return fmt.Errorf("GPU digest upsert: %w", err)
		}
	}
	if plugin.EnabledFor("node") {
		if err := ingestion.UpsertNodeDigests(ctx, pool, rows, orgID, clusterUUID); err != nil {
			return fmt.Errorf("node digest upsert: %w", err)
		}
	}
	return nil
}

func ProcessReport(msg *kafka.Message, consumer *kafka.Consumer) {
	log := logging.GetLogger()
	cfg := config.GetConfig()
	validate := validator.New()

	commitOnPermanentFailure := func(reason string) {
		payload := string(msg.Value)
		const maxPayloadLog = 65536
		if len(payload) > maxPayloadLog {
			payload = payload[:maxPayloadLog] + "…(truncated)"
		}
		log.Warnf("committing poison message (partition=%s, reason=%s); payload for manual recovery=%s", msg.TopicPartition, reason, payload)
		if consumer != nil {
			if _, err := consumer.CommitMessage(msg); err != nil {
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

	for _, file := range kafkaMsg.Files {
		csvType = utils.DetermineCSVType(file)

		if useNativeCSVIngest && csvType == types.PayloadTypeContainer {
			if err := processContainerCSVNative(file, kafkaMsg); err != nil {
				reportProcessingFailed = true
				recordKafkaTransient(err)
			}
			continue
		}
		if useNativeCSVIngest && csvType == types.PayloadTypeNamespace {
			if err := processNamespaceCSVNative(file, kafkaMsg); err != nil {
				reportProcessingFailed = true
				recordKafkaTransient(err)
			}
			continue
		}
		if useNativeCSVIngest && csvType == types.PayloadTypeStorage {
			if err := processStorageCSVNative(file, kafkaMsg); err != nil {
				reportProcessingFailed = true
				recordKafkaTransient(err)
			}
			continue
		}
		if useNativeCSVIngest && csvType == types.PayloadTypeSnapshot {
			if err := processSnapshotCSVNative(file, kafkaMsg); err != nil {
				reportProcessingFailed = true
				recordKafkaTransient(err)
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

	appCfg := config.GetConfig()
	if consumer != nil && !appCfg.KafkaAutoCommit {
		if kafkaTransientErr != nil {
			log.Errorf("kafka: offset not committed for partition=%s (transient processing error — message will be redelivered): %v",
				msg.TopicPartition, kafkaTransientErr)
		} else if _, err := consumer.CommitMessage(msg); err != nil {
			log.Errorf("kafka: unable to commit offset after successful processing (%s): %v", msg.TopicPartition, err)
		} else {
			metrics.KafkaMessagesProcessed.Inc()
		}
	} else if appCfg.KafkaAutoCommit && kafkaTransientErr == nil && !reportProcessingFailed {
		metrics.KafkaMessagesProcessed.Inc()
	}
}

// processContainerCSVNative handles container CSV files through the native Go
// recommendation engine instead of the Kruize pipeline. It downloads the CSV,
// computes daily digests, upserts them, and runs the recommendation engine.
func processContainerCSVNative(fileURL string, kafkaMsg types.KafkaMsg) error {
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	body, err := utils.ReadCSVBodyFromUrl(fileURL)
	if err != nil {
		csvFetchError.Inc()
		log.Errorf("native engine: unable to fetch CSV from URL: %v", err)
		if isTransientKafkaProcessingError(err) {
			return fmt.Errorf("fetch container CSV: %w", err)
		}
		return nil
	}
	defer body.Close()

	ctx := context.Background()
	pool := db.GetPool()

	tDigest := time.Now()
	handled, err := nativeCSVIngestViaPlugins(ctx, pool, body, orgID, clusterUUID, "container")
	if err != nil {
		log.Errorf("native engine: digest processing failed: %v", err)
		ingestionErrors.WithLabelValues("digest").Inc()
		if isTransientKafkaProcessingError(err) {
			return fmt.Errorf("digest processing: %w", err)
		}
		return nil
	}
	if !handled {
		if err := processContainerDigestFallback(ctx, pool, body, orgID, clusterUUID); err != nil {
			log.Errorf("native engine: digest processing failed: %v", err)
			ingestionErrors.WithLabelValues("digest").Inc()
			if isTransientKafkaProcessingError(err) {
				return fmt.Errorf("digest processing: %w", err)
			}
			return nil
		}
	}
	metrics.ObservePipelinePhase("digest", tDigest)

	now := time.Now().UTC()
	appCfg := config.GetConfig()
	start := now.AddDate(0, 0, -appCfg.MaxLookbackDays)
	oomCfg := engine.OOMConfig{
		BaseBump: appCfg.OOMBaseBump,
		MaxBump:  appCfg.OOMMaxBump,
	}

	costProvider := getCostDataProvider(appCfg)
	costData, costErr := costProvider.GetEffectiveRates(ctx, orgID, clusterUUID, start, now)
	if costErr != nil {
		log.Warnf("native engine: cost data fetch failed (NotifNoCostData applied via ApplySavingsEstimates): %v", costErr)
		costData = nil
	}

	oldRecs, err := engine.ReadClusterOldRecommendations(ctx, pool, orgID, clusterUUID)
	if err != nil {
		log.Errorf("native engine: reading old recommendations failed: %v", err)
		oldRecs = nil
	}

	engine.EnsureHistoryPartitions(ctx, pool)
	engine.EnsureQualityPartitions(ctx, pool)

	pipelineDegraded := false
	totalWritten := 0
	tRec := time.Now()

	err = engine.RecommendWorkloadsStreaming(ctx, pool, orgID, clusterUUID, start, now, oomCfg, func(batch []engine.ContainerRec) error {
		engine.ApplySavingsEstimates(batch, costData)

		if oldRecs != nil {
			adoptedKeys := engine.FindAdoptedContainers(batch, oldRecs)
			if markErr := engine.MarkAdopted(ctx, pool, orgID, clusterUUID, adoptedKeys); markErr != nil {
				log.Warnf("native engine: adoption marking incomplete: %v", markErr)
			}
		}

		if writeErr := engine.WriteRecommendations(ctx, pool, batch); writeErr != nil {
			ingestionErrors.WithLabelValues("write").Inc()
			return writeErr
		}
		totalWritten += len(batch)

		if histErr := engine.WriteRecommendationHistory(ctx, pool, batch, ""); histErr != nil {
			log.Errorf("native engine: writing recommendation history failed: %v", histErr)
			pipelineDegraded = true
		}

		if oldRecs != nil {
			oomCounts := engine.OOMCountsByContainer(batch)
			if qualErr := engine.WriteRecommendationQuality(ctx, pool, batch, oldRecs, oomCounts); qualErr != nil {
				log.Errorf("native engine: writing quality metrics failed: %v", qualErr)
				pipelineDegraded = true
			}
		}

		return nil
	})
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

	if oldRecs == nil {
		log.Warn("native engine: skipping quality metrics (old recs unavailable)")
		pipelineDegraded = true
	}

	if pipelineDegraded {
		log.Warn("native engine: analytics pipeline incomplete (history and/or quality) — container recommendations were written")
	}

	if plugin.EnabledFor("gpu") {
		tGPU := time.Now()
		if err := engine.MarkContainersWithGPU(ctx, pool, orgID, clusterUUID); err != nil {
			log.Warnf("native engine: marking GPU containers failed: %v", err)
		}
		gpuTerms, termErr := engine.LoadTermConfigCached(ctx, pool, orgID)
		if termErr != nil {
			log.Warnf("native engine: load term config for GPU classification failed: %v", termErr)
			gpuTerms = engine.DefaultTerms()
		}
		if err := engine.StoreGPUClassifications(ctx, pool, orgID, clusterUUID, gpuTerms); err != nil {
			log.Warnf("native engine: storing GPU classifications failed: %v", err)
		}
		metrics.ObservePipelinePhase("gpu_enrichment", tGPU)
	}

	if err := runNodeRecommendations(ctx, pool, orgID, clusterUUID, start, now, appCfg); err != nil {
		log.Warnf("native engine: node recommendations incomplete: %v", err)
		return fmt.Errorf("node recommendations: %w", err)
	}
	return nil
}

// runNodeRecommendations queries daily_node_digests for the cluster, computes
// Tier 1 node utilization signals, and persists the results.
func runNodeRecommendations(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, start, end time.Time, appCfg *config.Config) error {
	t0 := time.Now()
	defer func() { metrics.ObserveRecommendation("node", t0) }()

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

	terms, err := engine.LoadTermConfigCached(ctx, pool, orgID)
	if err != nil {
		log.Errorf("node recs: load term config failed, using defaults: %v", err)
		terms = engine.DefaultTerms()
	}

	cfg := engine.NodeRecConfig{
		UnderutilThreshold:         appCfg.NodeUnderutilThreshold,
		OvercommitThreshold:        appCfg.NodeOvercommitThreshold,
		AllocatableFactor:          appCfg.NodeAllocatableFactor,
		StrandedImbalanceThreshold: appCfg.NodeStrandedImbalanceThreshold,
		EMAAlpha:                   appCfg.NodeEMAAlpha,
	}
	recs := engine.RecommendNodes(digests, cfg, terms)
	if len(recs) == 0 {
		log.Info("node recs: no recommendations produced")
		return nil
	}

	validTerms := make([]string, len(terms))
	for i, tc := range terms {
		validTerms[i] = tc.Name
	}
	if err := engine.PersistNodeRecommendations(ctx, pool, orgID, clusterUUID, recs, validTerms); err != nil {
		log.Errorf("node recs: persist failed: %v", err)
		return fmt.Errorf("persist node recommendations: %w", err)
	}
	metrics.IncRecommendationsWritten("node", len(recs))
	return nil
}

// processNamespaceCSVNative handles namespace CSV files through the native Go
// recommendation engine instead of the Kruize pipeline.
func processNamespaceCSVNative(fileURL string, kafkaMsg types.KafkaMsg) error {
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	body, err := utils.ReadCSVBodyFromUrl(fileURL)
	if err != nil {
		csvFetchError.Inc()
		log.Errorf("native namespace engine: unable to fetch CSV from URL: %v", err)
		if isTransientKafkaProcessingError(err) {
			return fmt.Errorf("fetch namespace CSV: %w", err)
		}
		return nil
	}
	defer body.Close()

	ctx := context.Background()
	pool := db.GetPool()

	handled, err := nativeCSVIngestViaPlugins(ctx, pool, body, orgID, clusterUUID, "namespace")
	if err != nil {
		log.Errorf("native namespace engine: digest processing failed: %v", err)
		if isTransientKafkaProcessingError(err) {
			return fmt.Errorf("namespace digest processing: %w", err)
		}
		return nil
	}
	if !handled {
		if err := ingestion.ProcessNamespaceCSVToDigests(ctx, pool, body, orgID, clusterUUID); err != nil {
			log.Errorf("native namespace engine: digest processing failed: %v", err)
			if isTransientKafkaProcessingError(err) {
				return fmt.Errorf("namespace digest processing: %w", err)
			}
			return nil
		}
	}

	now := time.Now().UTC()
	nsCfg := config.GetConfig()
	start := now.AddDate(0, 0, -nsCfg.MaxLookbackDays)
	tNs := time.Now()
	results, err := engine.RecommendAllNamespaces(ctx, pool, orgID, clusterUUID, start, now)
	metrics.ObserveRecommendation("namespace", tNs)
	if err != nil {
		log.Errorf("native namespace engine: recommendation failed: %v", err)
		return fmt.Errorf("recommend namespaces: %w", err)
	}

	if len(results) == 0 {
		log.Info("native namespace engine: no recommendations produced")
		return nil
	}

	if err := engine.WriteNamespaceRecommendations(ctx, pool, results); err != nil {
		log.Errorf("native namespace engine: writing recommendations failed: %v", err)
		return fmt.Errorf("write namespace recommendations: %w", err)
	}
	metrics.IncRecommendationsWritten("namespace", len(results))
	log.Infof("native namespace engine: wrote %d recommendations", len(results))

	if err := engine.WriteNamespaceRecommendationHistory(ctx, pool, results); err != nil {
		log.Errorf("native namespace engine: writing history failed: %v", err)
		if isTransientKafkaProcessingError(err) {
			return fmt.Errorf("write namespace history: %w", err)
		}
	}
	return nil
}

func processStorageCSVNative(fileURL string, kafkaMsg types.KafkaMsg) error {
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	body, err := utils.ReadCSVBodyFromUrl(fileURL)
	if err != nil {
		csvFetchError.Inc()
		log.Errorf("native storage engine: unable to fetch CSV from URL: %v", err)
		if isTransientKafkaProcessingError(err) {
			return fmt.Errorf("fetch storage CSV: %w", err)
		}
		return nil
	}
	defer body.Close()

	ctx := context.Background()
	pool := db.GetPool()

	handled, err := nativeCSVIngestViaPlugins(ctx, pool, body, orgID, clusterUUID, string(types.PayloadTypeStorage))
	if err != nil {
		log.Errorf("native storage engine: digest processing failed: %v", err)
		if isTransientKafkaProcessingError(err) {
			return fmt.Errorf("storage digest processing: %w", err)
		}
		return nil
	}
	if !handled {
		if err := ingestion.ProcessStorageCSV(ctx, pool, body, orgID, clusterUUID); err != nil {
			log.Errorf("native storage engine: digest processing failed: %v", err)
			if isTransientKafkaProcessingError(err) {
				return fmt.Errorf("storage digest processing: %w", err)
			}
			return nil
		}
	}

	tPVC := time.Now()
	results, err := engine.RecommendPVCs(ctx, pool, orgID, clusterUUID)
	metrics.ObserveRecommendation("pvc", tPVC)
	if err != nil {
		log.Errorf("native storage engine: PVC recommendation failed: %v", err)
		return fmt.Errorf("recommend PVCs: %w", err)
	}

	if len(results) == 0 {
		log.Info("native storage engine: no PVC recommendations")
		return nil
	}

	if err := engine.WritePVCRecommendations(ctx, pool, results); err != nil {
		log.Errorf("native storage engine: writing PVC recommendations failed: %v", err)
		return fmt.Errorf("write PVC recommendations: %w", err)
	}
	metrics.IncRecommendationsWritten("pvc", len(results))
	log.Infof("native storage engine: wrote %d PVC recommendations", len(results))
	return nil
}

func processSnapshotCSVNative(fileURL string, kafkaMsg types.KafkaMsg) error {
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)

	body, err := utils.ReadCSVBodyFromUrl(fileURL)
	if err != nil {
		csvFetchError.Inc()
		log.Errorf("native snapshot engine: unable to fetch CSV from URL: %v", err)
		if isTransientKafkaProcessingError(err) {
			return fmt.Errorf("fetch snapshot CSV: %w", err)
		}
		return nil
	}
	defer body.Close()

	ctx := context.Background()
	pool := db.GetPool()

	handled, err := nativeCSVIngestViaPlugins(ctx, pool, body, orgID, clusterUUID, string(types.PayloadTypeSnapshot))
	if err != nil {
		log.Errorf("native snapshot engine: ingestion failed: %v", err)
		if isTransientKafkaProcessingError(err) {
			return fmt.Errorf("snapshot ingestion: %w", err)
		}
		return nil
	}
	if !handled {
		if err := ingestion.ProcessSnapshotCSV(ctx, pool, body, orgID, clusterUUID); err != nil {
			log.Errorf("native snapshot engine: ingestion failed: %v", err)
			if isTransientKafkaProcessingError(err) {
				return fmt.Errorf("snapshot ingestion: %w", err)
			}
			return nil
		}
	}

	settings, err := engine.ResolveSnapshotSettings(ctx, pool, orgID)
	if err != nil {
		log.Errorf("native snapshot engine: settings resolution failed: %v", err)
		return fmt.Errorf("snapshot settings: %w", err)
	}

	tSnap := time.Now()
	recs, err := engine.ClassifySnapshots(ctx, pool, orgID, clusterUUID, settings)
	metrics.ObserveRecommendation("snapshot", tSnap)
	if err != nil {
		log.Errorf("native snapshot engine: classification failed: %v", err)
		return fmt.Errorf("classify snapshots: %w", err)
	}

	if len(recs) > 0 {
		if err := engine.WriteSnapshotRecommendations(ctx, pool, recs); err != nil {
			log.Errorf("native snapshot engine: writing recommendations failed: %v", err)
			return fmt.Errorf("write snapshot recommendations: %w", err)
		}
		log.Infof("native snapshot engine: wrote %d snapshot recommendations", len(recs))
	}

	appCfg := config.GetConfig()
	removed, err := engine.ReconcileSnapshotRecommendations(ctx, pool, orgID, clusterUUID, appCfg.SnapshotStaleGraceHours)
	if err != nil {
		log.Errorf("native snapshot engine: reconciliation failed: %v", err)
		return fmt.Errorf("reconcile snapshots: %w", err)
	}
	if removed > 0 {
		log.Infof("native snapshot engine: reconciled (removed) %d stale recommendations", removed)
	}
	return nil
}

// getCostDataProvider returns a CostDataProvider based on configuration.
// Returns a NilCostDataProvider if KOKU_MASU_URL is not configured.
func getCostDataProvider(cfg *config.Config) costdata.CostDataProvider {
	if cfg.KokuMasuURL == "" {
		return &costdata.NilCostDataProvider{}
	}
	timeout := time.Duration(cfg.GlobalHTTPClientTimeoutSecs) * time.Second
	return costdata.NewHTTPCostDataProvider(cfg.KokuMasuURL, timeout)
}
