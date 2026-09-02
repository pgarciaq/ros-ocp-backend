package node

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	libnode "github.com/redhatinsights/ros-ocp-backend/librobne/node"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// NodeRecsAdvisoryLock is the pg_advisory_xact_lock key shared between
// PersistRecommendations and migration 000058 (PK rebuild) to prevent
// deadlocks without requiring manual worker shutdown during migrations.
const NodeRecsAdvisoryLock = pgrec.NodeRecsAdvisoryLock

// RecommendNodes evaluates node-level utilization signals from daily digest data.
func RecommendNodes(digests []DigestRow, cfg RecConfig, nodeSettings ThresholdSettings, terms []core.TermConfig) []Rec {
	return libnode.RecommendNodes(digests, cfg, nodeSettings, terms)
}

// ResolveAllocatable returns the effective allocatable CPU in millicores.
func ResolveAllocatable(storedAlloc *int64, maxRequests int64, factor float64) int64 {
	return libnode.ResolveAllocatable(storedAlloc, maxRequests, factor)
}

// ResolveAllocatableMem returns the effective allocatable memory in KiB.
func ResolveAllocatableMem(storedAlloc *int64, maxRequests int64, factor float64) int64 {
	return libnode.ResolveAllocatableMem(storedAlloc, maxRequests, factor)
}

// LinearRegressionSlope computes the slope of a simple OLS linear regression.
func LinearRegressionSlope(ys []float64) float64 {
	return libnode.LinearRegressionSlope(ys)
}

// QueryNodeDigests reads all_hours daily_node_digests for a cluster within a time range.
func QueryNodeDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, start, end time.Time) ([]DigestRow, error) {
	return queryNodeDigests(ctx, pool, orgID, clusterUUID, "", start, end, "all_hours")
}

// QueryNodeDigestsBySchedule reads daily_node_digests for one digest_schedule_type.
func QueryNodeDigestsBySchedule(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, start, end time.Time, scheduleType string) ([]DigestRow, error) {
	return queryNodeDigests(ctx, pool, orgID, clusterUUID, "", start, end, scheduleType)
}

// QueryNodeDigestsForNodeBySchedule reads one node's daily rows for a schedule type.
func QueryNodeDigestsForNodeBySchedule(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, nodeName string, start, end time.Time, scheduleType string) ([]DigestRow, error) {
	return queryNodeDigests(ctx, pool, orgID, clusterUUID, nodeName, start, end, scheduleType)
}

func queryNodeDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, nodeName string, start, end time.Time, scheduleType string) ([]DigestRow, error) {
	if scheduleType == "" {
		return nil, fmt.Errorf("query node digests: schedule_type is required")
	}
	rows, err := pool.Query(ctx, `
		SELECT bucket_date, node,
			COALESCE(cpu_usage_p50_mc, 0), COALESCE(cpu_usage_p95_mc, 0), COALESCE(cpu_usage_max_mc, 0),
			COALESCE(mem_usage_p50_kib, 0), COALESCE(mem_usage_p95_kib, 0), COALESCE(mem_usage_max_kib, 0),
			max_cpu_allocatable_mc, max_mem_allocatable_kib,
			COALESCE(max_cpu_requests_mc, 0), COALESCE(max_mem_requests_kib, 0),
			COALESCE(max_pod_count, 0), COALESCE(pod_capacity, 0),
			COALESCE(instance_type, ''), COALESCE(machineset_name, ''),
			COALESCE(sample_count, 0), node_gpu_count
		FROM daily_node_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = $5
		  AND ($6 = '' OR node = $6)
		ORDER BY node, bucket_date`,
		orgID, clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"), scheduleType, nodeName)
	// N.B. filterNodeByWindow uses binary search and relies on bucket_date sort order above.
	if err != nil {
		return nil, fmt.Errorf("query node digests: %w", err)
	}
	defer rows.Close()

	result := make([]DigestRow, 0, libnode.DefaultDigestCapacity)
	for rows.Next() {
		var d DigestRow
		err := rows.Scan(
			&d.BucketDate, &d.Node,
			&d.CPUUsageP50MC, &d.CPUUsageP95MC, &d.CPUUsageMaxMC,
			&d.MemUsageP50KiB, &d.MemUsageP95KiB, &d.MemUsageMaxKiB,
			&d.MaxCPUAllocMC, &d.MaxMemAllocKiB,
			&d.MaxCPURequestsMC, &d.MaxMemRequestsKiB,
			&d.MaxPodCount, &d.PodCapacity, &d.InstanceType, &d.MachineSetName, &d.SampleCount,
			&d.NodeGPUCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan node digest row: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node digest rows: %w", err)
	}
	return result, nil
}

// PersistRecommendations upserts computed node recommendations into the database.
func PersistRecommendations(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, recs []Rec, validTerms []string) error {
	if len(recs) == 0 {
		return nil
	}

	t0 := time.Now()
	defer func() { metrics.ObserveDB("persist_node_recommendations", t0) }()

	if err := pgrec.WriteNodeRecommendations(ctx, pool, orgID, clusterUUID, recs, validTerms); err != nil {
		return err
	}

	logging.ForOrg(orgID, clusterUUID).Infof("PersistRecommendations: upserted %d recs", len(recs))
	return nil
}
