package housekeeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	k "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"gorm.io/gorm"

	"github.com/redhatinsights/ros-ocp-backend/internal/clustercache"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/kafka"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils/kruize"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils/sources"
)

// analyticsCleanupBatchSize caps how many rows each DELETE removes per transaction when cleaning up a
// cluster. Large tenants can accumulate millions of digest rows; batching avoids a single huge transaction
// (WAL growth, long locks) on a shared PostgreSQL instance.
const analyticsCleanupBatchSize = 5000

var cost_app_id int

// deleteMatchingInBatches deletes rows matching whereClause using repeated batched DELETEs. Table must be
// a trusted identifier (literal from cleanupClusterAnalytics only).
//
// PK-based batching is used instead of ctid so deletes behave correctly on partitioned tables (partition
// pruning-friendly, stable row identity) and remain portable across PostgreSQL versions.
func deleteMatchingInBatches(db *gorm.DB, stepName, table string, pkColumns []string, whereClause string, args []any, orgID, clusterUUID string) error {
	if len(pkColumns) == 0 {
		return fmt.Errorf("%s: pkColumns required", stepName)
	}
	pkList := strings.Join(pkColumns, ", ")
	var total int64
	for {
		sql := fmt.Sprintf(
			`DELETE FROM %s WHERE (%s) IN (SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT ?)`,
			table, pkList, pkList, table, whereClause, pkList,
		)
		qArgs := append(append([]any{}, args...), analyticsCleanupBatchSize)
		res := db.Exec(sql, qArgs...)
		if res.Error != nil {
			return fmt.Errorf("%s: %w", stepName, res.Error)
		}
		if res.RowsAffected == 0 {
			break
		}
		total += res.RowsAffected
		if res.RowsAffected < int64(analyticsCleanupBatchSize) {
			break
		}
	}
	logging.ForOrg(orgID, clusterUUID).Infof("sources cleanup: %s deleted %d rows total", stepName, total)
	return nil
}

func cleanupClusterAnalytics(db *gorm.DB, orgID, clusterUUID string) error {
	// Each step commits independently (batched DELETEs per table). On failure mid-run, already-deleted
	// batches stay deleted; callers may need to retry cleanup if the Sources destroy event is not replayed.
	steps := []struct {
		name   string
		table  string
		pkCols []string
		where  string
		args   []any
	}{
		{"daily_container_digests", "daily_container_digests", []string{"org_id", "cluster_uuid", "namespace", "workload", "container_name", "bucket_date"}, `org_id = ? AND cluster_uuid = ?::uuid`, []any{orgID, clusterUUID}},
		{"daily_namespace_digests", "daily_namespace_digests", []string{"org_id", "cluster_uuid", "namespace", "bucket_date"}, `org_id = ? AND cluster_uuid = ?::uuid`, []any{orgID, clusterUUID}},
		{"daily_pvc_digests", "daily_pvc_digests", []string{"id", "bucket_date"}, `org_id = ? AND cluster_uuid = ?::uuid`, []any{orgID, clusterUUID}},
		{"daily_node_digests", "daily_node_digests", []string{"org_id", "cluster_uuid", "node", "bucket_date"}, `org_id = ? AND cluster_uuid = ?::uuid`, []any{orgID, clusterUUID}},
		{"gpu_container_digests", "gpu_container_digests", []string{"id", "interval_start"}, `cluster_uuid = ?::uuid`, []any{clusterUUID}},
		{"node_gpu_timeslicing_recommendations", "node_gpu_timeslicing_recommendations", []string{"org_id", "cluster_uuid", "node_name", "gpu_model", "term"}, `org_id = ? AND cluster_uuid = ?::uuid`, []any{orgID, clusterUUID}},
		{"node_gpu_timeslicing_recommendation_history", "node_gpu_timeslicing_recommendation_history", []string{"id"}, `org_id = ? AND cluster_uuid = ?::uuid`, []any{orgID, clusterUUID}},
		{"recommendation_quality", "recommendation_quality", []string{"org_id", "cluster_uuid", "namespace", "workload", "container_name", "measured_at"}, `org_id = ? AND cluster_uuid = ?::uuid`, []any{orgID, clusterUUID}},
		{"recommendation_history", "recommendation_history", []string{"org_id", "cluster_uuid", "namespace", "workload", "container_name", "term", "engine", "recorded_at"}, `org_id = ? AND cluster_uuid = ?::uuid`, []any{orgID, clusterUUID}},
		{"pvc_recommendation_sets", "pvc_recommendation_sets", []string{"id"}, `org_id = ? AND cluster_uuid = ?::uuid`, []any{orgID, clusterUUID}},
		{"recommendation_sets", "recommendation_sets", []string{"org_id", "cluster_uuid", "namespace", "workload", "container_name", "term", "engine"}, `org_id = ? AND cluster_uuid = ?`, []any{orgID, clusterUUID}},
		{"snapshot_inventory", "snapshot_inventory", []string{"id"}, `org_id = ? AND cluster_uuid = ?::uuid`, []any{orgID, clusterUUID}},
		{"snapshot_recommendation_sets", "snapshot_recommendation_sets", []string{"id"}, `org_id = ? AND cluster_uuid = ?::uuid`, []any{orgID, clusterUUID}},
		{"node_recommendations", "node_recommendations", []string{"org_id", "cluster_uuid", "node", "term", "engine"}, `org_id = ? AND cluster_uuid = ?::uuid`, []any{orgID, clusterUUID}},
		// Kruize-era tables: delete children before workloads to avoid CASCADE fan-out.
		// workload_metrics and historical_recommendation_sets reference workloads(id).
		{"workload_metrics", "workload_metrics", []string{"id"}, `workload_id IN (SELECT id FROM workloads WHERE cluster_id = (SELECT id FROM clusters WHERE cluster_uuid = ?::uuid LIMIT 1))`, []any{clusterUUID}},
		{"historical_recommendation_sets", "historical_recommendation_sets", []string{"id"}, `workload_id IN (SELECT id FROM workloads WHERE cluster_id = (SELECT id FROM clusters WHERE cluster_uuid = ?::uuid LIMIT 1))`, []any{clusterUUID}},
		// Finally delete workloads themselves so clusters CASCADE is a no-op.
		{"workloads", "workloads", []string{"id"}, `cluster_id = (SELECT id FROM clusters WHERE cluster_uuid = ?::uuid LIMIT 1)`, []any{clusterUUID}},
	}
	for _, step := range steps {
		if err := deleteMatchingInBatches(db, step.name, step.table, step.pkCols, step.where, step.args, orgID, clusterUUID); err != nil {
			return err
		}
	}
	fleetsummary.InvalidateOrg(orgID)
	fleetheatmap.InvalidateOrg(orgID)
	clustercache.InvalidateOrg(orgID)
	return nil
}

func StartSourcesListenerService(ctx context.Context) {
	log := logging.GetLogger()
	cfg := config.GetConfig()

	var err error
	cost_app_id, err = sources.GetCostApplicationID()
	if err != nil {
		log.Fatalf("Unable to get cost application id: %v", err)
	}

	kafka.StartConsumer(ctx, cfg.SourcesEventTopic, sourcesListener)

	if ctx.Err() != nil {
		log.Info("shutting down housekeeper gracefully")
		waitHousekeeperShutdownGrace(cfg.HousekeeperShutdownGraceSecs)
	}
}

func waitHousekeeperShutdownGrace(secs int) {
	if secs <= 0 {
		return
	}
	time.Sleep(time.Duration(secs) * time.Second)
}

func sourcesListener(_ context.Context, msg *k.Message, _ *k.Consumer) {
	db := database.GetDB()
	log := logging.GetLogger()
	headers := msg.Headers
	for _, v := range headers {
		if v.Key == "event_type" && string(v.Value) == "Application.destroy" {
			var data types.SourcesEvent
			if !json.Valid([]byte(msg.Value)) {
				log.Errorf("Received message on kafka topic is not valid JSON (len=%d)", len(msg.Value))
				return
			}
			if err := json.Unmarshal(msg.Value, &data); err != nil {
				log.Errorf("Unable to decode kafka message (len=%d): %v", len(msg.Value), err)
				return
			}
			if data.Application_type_id == cost_app_id {
				var cluster model.Cluster
				if err := db.Where("source_id = ?", strconv.Itoa(data.Source_id)).First(&cluster).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						log.Infof("no cluster found for source_id=%d; nothing to clean up", data.Source_id)
					} else {
						log.Errorf("unable to look up cluster for source_id=%d: %v", data.Source_id, err)
					}
					return
				}
				var account model.RHAccount
				if err := db.First(&account, cluster.TenantID).Error; err != nil {
					log.Errorf("unable to resolve rh_accounts row for tenant_id=%d: %v", cluster.TenantID, err)
					return
				}
				if err := cleanupClusterAnalytics(db, account.OrgId, cluster.ClusterUUID); err != nil {
					logging.ForOrg(account.OrgId, cluster.ClusterUUID).Errorf("analytics cleanup failed: %v", err)
					return
				}
				workloads, err := model.GetWorkloadsByClusterID(cluster.ID)
				if err != nil {
					log.Errorf("unable to get workloads for cluster: %v. Error: %v", cluster, err)
					return
				}

				for _, workload := range workloads {
					kruize.DeleteExperimentFromKruize(workload.ExperimentName)
				}

				if err := cluster.DeleteCluster(); err != nil {
					log.Errorf("unable to delete record from clusters table: %v. Error: %v", cluster, err)
				} else {
					log.Infof("Successfully deleted the cluster with Source_id: %v.", cluster.SourceId)
				}
			}

		}
	}
}
