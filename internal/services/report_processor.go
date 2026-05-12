package services

import (
	"context"
	"encoding/json"
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
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
	"github.com/redhatinsights/ros-ocp-backend/internal/types/kruizePayload"
	namespacePayload "github.com/redhatinsights/ros-ocp-backend/internal/types/kruizePayload/namespace"
	w "github.com/redhatinsights/ros-ocp-backend/internal/types/workload"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils/kruize"
)

var cfg *config.Config = config.GetConfig()

func ProcessReport(msg *kafka.Message, consumer *kafka.Consumer) {
	log := logging.GetLogger()
	cfg = config.GetConfig()
	validate := validator.New()

	commitOnPermanentFailure := func(reason string) {
		log.Warnf("committing poison message (partition=%s, reason=%s)", msg.TopicPartition, reason)
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

	// Create RHAccount and Cluster once before the file loop so both native
	// and legacy paths have valid rows for JOINs in the API read path.
	rhAccount := model.RHAccount{
		Account: kafkaMsg.Metadata.Account,
		OrgId:   kafkaMsg.Metadata.Org_id,
	}
	if err := rhAccount.CreateRHAccount(); err != nil {
		log.Errorf("unable to get or add record to rh_accounts table: %v. Error: %v", rhAccount, err)
		return
	}

	cluster := model.Cluster{
		TenantID:       rhAccount.ID,
		SourceId:       kafkaMsg.Metadata.Source_id,
		ClusterUUID:    kafkaMsg.Metadata.Cluster_uuid,
		ClusterAlias:   kafkaMsg.Metadata.Cluster_alias,
		LastReportedAt: time.Now(),
	}
	if err := cluster.CreateCluster(); err != nil {
		log.Errorf("unable to get or add record to clusters table: %v. Error: %v", cluster, err)
		return
	}

	var csvType types.PayloadType

	for _, file := range kafkaMsg.Files {
		csvType = utils.DetermineCSVType(file)

		if cfg.UseNativeEngine && csvType == types.PayloadTypeContainer {
			processContainerCSVNative(file, kafkaMsg)
			continue
		}
		if cfg.UseNativeEngine && csvType == types.PayloadTypeNamespace {
			processNamespaceCSVNative(file, kafkaMsg)
			continue
		}
		if cfg.UseNativeEngine && csvType == types.PayloadTypeStorage {
			processStorageCSVNative(file, kafkaMsg)
			continue
		}
		if cfg.UseNativeEngine && csvType == types.PayloadTypeSnapshot {
			processSnapshotCSVNative(file, kafkaMsg)
			continue
		}

		data, fetchError := utils.ReadCSVFromUrl(file)
		if fetchError != nil {
			csvFetchError.Inc()
			log.Errorf("unable to read CSV from URL: %s", fetchError.Error())
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

}

// processContainerCSVNative handles container CSV files through the native Go
// recommendation engine instead of the Kruize pipeline. It downloads the CSV,
// computes daily digests, upserts them, and runs the recommendation engine.
func processContainerCSVNative(fileURL string, kafkaMsg types.KafkaMsg) {
	log := logging.GetLogger()
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid

	body, err := utils.ReadCSVBodyFromUrl(fileURL)
	if err != nil {
		csvFetchError.Inc()
		log.Errorf("native engine: unable to fetch CSV from URL: %v", err)
		return
	}
	defer body.Close()

	ctx := context.Background()
	pool := db.GetPool()

	if err := ingestion.ProcessCSVToDigests(ctx, pool, body, orgID, clusterUUID); err != nil {
		log.Errorf("native engine: digest processing failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return
	}

	now := time.Now().UTC()
	appCfg := config.GetConfig()
	start := now.AddDate(0, 0, -appCfg.MaxLookbackDays)
	oomCfg := engine.OOMConfig{
		BaseBump: appCfg.OOMBaseBump,
		MaxBump:  appCfg.OOMMaxBump,
	}
	results, err := engine.RecommendAllWorkloads(ctx, pool, orgID, clusterUUID, start, now, oomCfg)
	if err != nil {
		log.Errorf("native engine: recommendation failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return
	}

	if len(results) == 0 {
		log.Infof("native engine: no recommendations produced for org=%s cluster=%s", orgID, clusterUUID)
		return
	}

	// Step 0.5: Fetch cost data from Koku and compute savings estimates.
	// clusterUUID in ROS is the same as cluster_id in Koku (OpenShift cluster ID).
	costProvider := getCostDataProvider(appCfg)
	costData, err := costProvider.GetEffectiveRates(ctx, orgID, clusterUUID, start, now)
	if err != nil {
		log.Warnf("native engine: cost data fetch failed for org=%s cluster=%s: %v (savings will be zero)", orgID, clusterUUID, err)
	}
	engine.ApplySavingsEstimates(results, costData)

	// Step 1: Read old recommendations before overwrite (for stability_pct, adoption_detected)
	containerKeys := engine.ContainerKeys(results)
	oldRecs, err := engine.ReadOldRecommendations(ctx, pool, orgID, clusterUUID, containerKeys)
	if err != nil {
		log.Errorf("native engine: reading old recommendations failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
	}

	// Step 1b: Adoption detection — compare current resource values against prior recommendations.
	if oldRecs != nil {
		adoptedKeys := engine.FindAdoptedContainers(results, oldRecs)
		engine.MarkAdopted(ctx, pool, orgID, clusterUUID, adoptedKeys)
	}

	// Step 2: Write new recommendations (overwrites old values)
	if err := engine.WriteRecommendations(ctx, pool, results); err != nil {
		log.Errorf("native engine: writing recommendations failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return
	}
	log.Infof("native engine: wrote %d recommendations for org=%s cluster=%s", len(results), orgID, clusterUUID)

	// Step 2b: Snapshot recommendations into history (non-fatal on failure).
	engine.EnsureHistoryPartitions(ctx, pool)
	if err := engine.WriteRecommendationHistory(ctx, pool, results, ""); err != nil {
		log.Errorf("native engine: writing recommendation history failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
	}

	// Step 3: Write recommendation quality metrics (non-blocking for primary pipeline).
	// Skip quality writes if old recommendations could not be read -- writing with
	// "no prior rec" defaults would produce misleading stability/adoption metrics.
	if oldRecs == nil {
		log.Warnf("native engine: skipping quality metrics (old recs unavailable) for org=%s cluster=%s", orgID, clusterUUID)
		return
	}
	engine.EnsureQualityPartitions(ctx, pool)
	oomCounts := engine.OOMCountsByContainer(results)
	if err := engine.WriteRecommendationQuality(ctx, pool, results, oldRecs, oomCounts); err != nil {
		log.Errorf("native engine: writing quality metrics failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
	}

	// Step 4: Node-level recommendations (Tier 1 right-sizing).
	runNodeRecommendations(ctx, pool, orgID, clusterUUID, start, now, appCfg)
}

// runNodeRecommendations queries daily_node_digests for the cluster, computes
// Tier 1 node utilization signals, and persists the results.
func runNodeRecommendations(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, start, end time.Time, appCfg *config.Config) {
	log := logging.GetLogger()

	digests, err := engine.QueryNodeDigests(ctx, pool, orgID, clusterUUID, start, end)
	if err != nil {
		log.Warnf("node recs: query digests failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return
	}
	if len(digests) == 0 {
		log.Infof("node recs: no node digests for org=%s cluster=%s", orgID, clusterUUID)
		return
	}

	cfg := engine.NodeRecConfig{
		UnderutilThreshold:         appCfg.NodeUnderutilThreshold,
		OvercommitThreshold:        appCfg.NodeOvercommitThreshold,
		AllocatableFactor:          appCfg.NodeAllocatableFactor,
		MinDataDays:                appCfg.NodeMinDataDays,
		StrandedImbalanceThreshold: appCfg.NodeStrandedImbalanceThreshold,
		EMAAlpha:                   appCfg.NodeEMAAlpha,
	}
	recs := engine.RecommendNodes(digests, cfg)
	if len(recs) == 0 {
		log.Infof("node recs: no recommendations produced for org=%s cluster=%s", orgID, clusterUUID)
		return
	}

	if err := engine.PersistNodeRecommendations(ctx, pool, orgID, clusterUUID, recs); err != nil {
		log.Errorf("node recs: persist failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
	}
}

// processNamespaceCSVNative handles namespace CSV files through the native Go
// recommendation engine instead of the Kruize pipeline.
func processNamespaceCSVNative(fileURL string, kafkaMsg types.KafkaMsg) {
	log := logging.GetLogger()
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid

	body, err := utils.ReadCSVBodyFromUrl(fileURL)
	if err != nil {
		csvFetchError.Inc()
		log.Errorf("native namespace engine: unable to fetch CSV from URL: %v", err)
		return
	}
	defer body.Close()

	ctx := context.Background()
	pool := db.GetPool()

	if err := ingestion.ProcessNamespaceCSVToDigests(ctx, pool, body, orgID, clusterUUID); err != nil {
		log.Errorf("native namespace engine: digest processing failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return
	}

	now := time.Now().UTC()
	nsCfg := config.GetConfig()
	start := now.AddDate(0, 0, -nsCfg.MaxLookbackDays)
	results, err := engine.RecommendAllNamespaces(ctx, pool, orgID, clusterUUID, start, now)
	if err != nil {
		log.Errorf("native namespace engine: recommendation failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return
	}

	if len(results) == 0 {
		log.Infof("native namespace engine: no recommendations produced for org=%s cluster=%s", orgID, clusterUUID)
		return
	}

	if err := engine.WriteNamespaceRecommendations(ctx, pool, results); err != nil {
		log.Errorf("native namespace engine: writing recommendations failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return
	}
	log.Infof("native namespace engine: wrote %d recommendations for org=%s cluster=%s", len(results), orgID, clusterUUID)

	if err := engine.WriteNamespaceRecommendationHistory(ctx, pool, results); err != nil {
		log.Errorf("native namespace engine: writing history failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
	}
}

func processStorageCSVNative(fileURL string, kafkaMsg types.KafkaMsg) {
	log := logging.GetLogger()
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid

	body, err := utils.ReadCSVBodyFromUrl(fileURL)
	if err != nil {
		csvFetchError.Inc()
		log.Errorf("native storage engine: unable to fetch CSV from URL: %v", err)
		return
	}
	defer body.Close()

	ctx := context.Background()
	pool := db.GetPool()

	if err := ingestion.ProcessStorageCSV(ctx, pool, body, orgID, clusterUUID); err != nil {
		log.Errorf("native storage engine: digest processing failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return
	}

	results, err := engine.RecommendPVCs(ctx, pool, orgID, clusterUUID)
	if err != nil {
		log.Errorf("native storage engine: PVC recommendation failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return
	}

	if len(results) == 0 {
		log.Infof("native storage engine: no PVC recommendations for org=%s cluster=%s", orgID, clusterUUID)
		return
	}

	if err := engine.WritePVCRecommendations(ctx, pool, results); err != nil {
		log.Errorf("native storage engine: writing PVC recommendations failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return
	}
	log.Infof("native storage engine: wrote %d PVC recommendations for org=%s cluster=%s", len(results), orgID, clusterUUID)
}

func processSnapshotCSVNative(fileURL string, kafkaMsg types.KafkaMsg) {
	log := logging.GetLogger()
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid

	body, err := utils.ReadCSVBodyFromUrl(fileURL)
	if err != nil {
		csvFetchError.Inc()
		log.Errorf("native snapshot engine: unable to fetch CSV from URL: %v", err)
		return
	}
	defer body.Close()

	ctx := context.Background()
	pool := db.GetPool()

	if err := ingestion.ProcessSnapshotCSV(ctx, pool, body, orgID, clusterUUID); err != nil {
		log.Errorf("native snapshot engine: ingestion failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return
	}

	// Resolve settings for this org
	settings, err := engine.ResolveSnapshotSettings(ctx, pool, orgID)
	if err != nil {
		log.Errorf("native snapshot engine: settings resolution failed for org=%s: %v", orgID, err)
		return
	}

	// Classify snapshots
	recs, err := engine.ClassifySnapshots(ctx, pool, orgID, clusterUUID, settings)
	if err != nil {
		log.Errorf("native snapshot engine: classification failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return
	}

	if len(recs) > 0 {
		if err := engine.WriteSnapshotRecommendations(ctx, pool, recs); err != nil {
			log.Errorf("native snapshot engine: writing recommendations failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
			return
		}
		log.Infof("native snapshot engine: wrote %d snapshot recommendations for org=%s cluster=%s", len(recs), orgID, clusterUUID)
	}

	// Reconcile: remove recommendations for snapshots no longer in inventory
	removed, err := engine.ReconcileSnapshotRecommendations(ctx, pool, orgID, clusterUUID)
	if err != nil {
		log.Errorf("native snapshot engine: reconciliation failed for org=%s cluster=%s: %v", orgID, clusterUUID, err)
		return
	}
	if removed > 0 {
		log.Infof("native snapshot engine: reconciled (removed) %d stale recommendations for org=%s cluster=%s", removed, orgID, clusterUUID)
	}
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
