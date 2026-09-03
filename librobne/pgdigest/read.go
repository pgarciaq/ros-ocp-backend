package pgdigest

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// Querier is implemented by *pgxpool.Pool and pgx.Tx.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// containerDigestSelectMetrics is the shared SELECT list through cpu_usage_cv_bp.
// One-cluster and multi-cluster queries append identity columns after this.
const containerDigestSelectMetrics = `
		SELECT bucket_date,
			COALESCE(cpu_request_p50_mc, 0), COALESCE(cpu_request_p60_mc, 0),
			COALESCE(cpu_request_p95_mc, 0), COALESCE(cpu_request_p98_mc, 0), COALESCE(cpu_request_p99_mc, 0),
			COALESCE(cpu_usage_p50_mc, 0), COALESCE(cpu_usage_p60_mc, 0),
			COALESCE(cpu_usage_p95_mc, 0), COALESCE(cpu_usage_p98_mc, 0), COALESCE(cpu_usage_p99_mc, 0),
			COALESCE(cpu_usage_max_mc, 0),
			COALESCE(cpu_throttle_p95_mc, 0), COALESCE(cpu_throttle_max_mc, 0),
			COALESCE(memory_request_p50_kib, 0), COALESCE(memory_request_p60_kib, 0),
			COALESCE(memory_request_p95_kib, 0), COALESCE(memory_request_p98_kib, 0), COALESCE(memory_request_p99_kib, 0),
			COALESCE(memory_usage_p50_kib, 0), COALESCE(memory_usage_p60_kib, 0),
			COALESCE(memory_usage_p95_kib, 0), COALESCE(memory_usage_p98_kib, 0), COALESCE(memory_usage_p99_kib, 0),
			COALESCE(memory_usage_max_kib, 0),
			COALESCE(memory_rss_p95_kib, 0), COALESCE(memory_rss_max_kib, 0),
			COALESCE(oom_count_sum, 0), COALESCE(cpu_usage_mean_mc, 0), COALESCE(memory_usage_mean_kib, 0),
			COALESCE(sample_count, 0),
			COALESCE(pod_count_min, 0), COALESCE(pod_count_max, 0), COALESCE(pod_count_avg, 0),
			COALESCE(desired_replicas, 0), COALESCE(available_replicas, 0),
			cpu_usage_cv_bp`

// SelectAllHours is the recommend-path digest query: one cluster, one
// schedule_type ($5). CLI Path A/B pass all_hours or business_hours.
const SelectAllHours = containerDigestSelectMetrics + `,
			namespace, workload, workload_type, container_name
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = $5
		ORDER BY namespace, workload, workload_type, container_name, bucket_date`

const selectAllHoursMultiClusterWhere = containerDigestSelectMetrics + `,
			cluster_uuid::text, namespace, workload, workload_type, container_name
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = ANY($2::uuid[])
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = $5`

// SelectAllHoursMultiCluster is the same metric list as SelectAllHours for
// cluster_uuid = ANY($2::uuid[]). Processor groups; do not buffer then filter.
const SelectAllHoursMultiCluster = selectAllHoursMultiClusterWhere + `
		ORDER BY cluster_uuid, namespace, workload, workload_type, container_name, bucket_date`

// SelectAllHoursForContainers is the page-key digest query. The unnest key
// omits workload_type (product lock — do not "fix" here).
const SelectAllHoursForContainers = selectAllHoursMultiClusterWhere + `
		  AND (cluster_uuid, namespace, workload, container_name) IN (
			SELECT u.c, u.n, u.w, u.cn
			FROM unnest($6::uuid[], $7::text[], $8::text[], $9::text[]) AS u(c, n, w, cn)
		  )
		ORDER BY cluster_uuid, namespace, workload, workload_type, container_name, bucket_date`

// ContainerPageKey identifies a container on an API page for scoped digest lookups.
type ContainerPageKey struct {
	ClusterUUID   string
	Namespace     string
	Workload      string
	ContainerName string
}

const dateLayout = "2006-01-02"

func ReadContainerDigests(ctx context.Context, q Querier, orgID, clusterUUID string, start, end time.Time) ([]types.KeyedDigest, error) {
	return ReadContainerDigestsBySchedule(ctx, q, orgID, clusterUUID, start, end, ScheduleAllHours)
}

// ReadContainerDigestsBySchedule loads container days for one schedule_type.
func ReadContainerDigestsBySchedule(ctx context.Context, q Querier, orgID, clusterUUID string, start, end time.Time, scheduleType string) ([]types.KeyedDigest, error) {
	const defaultDigestRowCapacity = 8192
	out := make([]types.KeyedDigest, 0, defaultDigestRowCapacity)
	err := ForEachSchedule(ctx, q, orgID, clusterUUID, start, end, scheduleType, func(d types.KeyedDigest) error {
		out = append(out, d)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ForEachAllHours streams all_hours digest rows. The caller must consume the
// callback to completion before other SQL on the same connection (ADR-0171).
func ForEachAllHours(ctx context.Context, q Querier, orgID, clusterUUID string, start, end time.Time, fn func(types.KeyedDigest) error) error {
	return ForEachSchedule(ctx, q, orgID, clusterUUID, start, end, ScheduleAllHours, fn)
}

// ForEachSchedule streams digest rows for scheduleType. The caller must consume
// the callback to completion before other SQL on the same connection (ADR-0171).
func ForEachSchedule(ctx context.Context, q Querier, orgID, clusterUUID string, start, end time.Time, scheduleType string, fn func(types.KeyedDigest) error) error {
	if orgID == "" {
		return fmt.Errorf("pgdigest: org_id is required")
	}
	if clusterUUID == "" {
		return fmt.Errorf("pgdigest: cluster_uuid is required")
	}
	if scheduleType == "" {
		return fmt.Errorf("pgdigest: schedule_type is required")
	}
	if q == nil {
		return fmt.Errorf("pgdigest: querier is required")
	}
	if fn == nil {
		return fmt.Errorf("pgdigest: callback is required")
	}
	rows, err := q.Query(ctx, SelectAllHours, orgID, clusterUUID, start.Format(dateLayout), end.Format(dateLayout), scheduleType)
	if err != nil {
		return fmt.Errorf("pgdigest: query digests: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		d, err := ScanKeyedDigest(rows)
		if err != nil {
			return err
		}
		if err := fn(d); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("pgdigest: iterate digest rows: %w", err)
	}
	return nil
}

func validateScheduleQuerier(orgID, scheduleType string, q Querier) error {
	if orgID == "" {
		return fmt.Errorf("pgdigest: org_id is required")
	}
	if scheduleType == "" {
		return fmt.Errorf("pgdigest: schedule_type is required")
	}
	if q == nil {
		return fmt.Errorf("pgdigest: querier is required")
	}
	return nil
}

// ForEachScheduleForClusters streams digest rows for cluster_uuid = ANY(clusterUUIDs).
// Empty clusterUUIDs is a no-op. The caller must consume the callback to completion
// before other SQL on the same connection (ADR-0171).
func ForEachScheduleForClusters(
	ctx context.Context,
	q Querier,
	orgID string,
	clusterUUIDs []string,
	start, end time.Time,
	scheduleType string,
	fn func(clusterUUID string, d types.KeyedDigest) error,
) error {
	if len(clusterUUIDs) == 0 {
		return nil
	}
	if fn == nil {
		return fmt.Errorf("pgdigest: callback is required")
	}
	if err := validateScheduleQuerier(orgID, scheduleType, q); err != nil {
		return err
	}
	rows, err := q.Query(ctx, SelectAllHoursMultiCluster, orgID, clusterUUIDs, start.Format(dateLayout), end.Format(dateLayout), scheduleType)
	if err != nil {
		return fmt.Errorf("pgdigest: query digests: %w", err)
	}
	return iterateClusterKeyed(rows, fn)
}

// ForEachScheduleForContainers streams digest rows for page container keys.
// Empty keys is a no-op. Does not load a cluster and filter in Go.
func ForEachScheduleForContainers(
	ctx context.Context,
	q Querier,
	orgID string,
	keys []ContainerPageKey,
	start, end time.Time,
	scheduleType string,
	fn func(clusterUUID string, d types.KeyedDigest) error,
) error {
	if len(keys) == 0 {
		return nil
	}
	if fn == nil {
		return fmt.Errorf("pgdigest: callback is required")
	}
	if err := validateScheduleQuerier(orgID, scheduleType, q); err != nil {
		return err
	}
	clusterUUIDs := make([]string, len(keys))
	namespaces := make([]string, len(keys))
	workloads := make([]string, len(keys))
	containers := make([]string, len(keys))
	uniqueClusters := make([]string, 0)
	seenCluster := make(map[string]bool)
	for i, key := range keys {
		clusterUUIDs[i] = key.ClusterUUID
		namespaces[i] = key.Namespace
		workloads[i] = key.Workload
		containers[i] = key.ContainerName
		if !seenCluster[key.ClusterUUID] {
			seenCluster[key.ClusterUUID] = true
			uniqueClusters = append(uniqueClusters, key.ClusterUUID)
		}
	}
	rows, err := q.Query(ctx, SelectAllHoursForContainers,
		orgID, uniqueClusters, start.Format(dateLayout), end.Format(dateLayout), scheduleType,
		clusterUUIDs, namespaces, workloads, containers)
	if err != nil {
		return fmt.Errorf("pgdigest: query digests: %w", err)
	}
	return iterateClusterKeyed(rows, fn)
}

func iterateClusterKeyed(rows pgx.Rows, fn func(clusterUUID string, d types.KeyedDigest) error) error {
	defer rows.Close()
	for rows.Next() {
		clusterUUID, d, err := ScanKeyedDigestCluster(rows)
		if err != nil {
			return err
		}
		if err := fn(clusterUUID, d); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("pgdigest: iterate digest rows: %w", err)
	}
	return nil
}

// ScanKeyedDigest scans one recommend-path digest row (SelectAllHours column order).
func ScanKeyedDigest(rows pgx.Rows) (types.KeyedDigest, error) {
	var d types.DigestRow
	var ns, wl, wlType, cn string
	if err := rows.Scan(
		&d.BucketDate,
		&d.CPURequestP50MC, &d.CPURequestP60MC, &d.CPURequestP95MC, &d.CPURequestP98MC, &d.CPURequestP99MC,
		&d.CPUUsageP50MC, &d.CPUUsageP60MC, &d.CPUUsageP95MC, &d.CPUUsageP98MC, &d.CPUUsageP99MC, &d.CPUUsageMaxMC,
		&d.CPUThrottleP95MC, &d.CPUThrottleMaxMC,
		&d.MemRequestP50KiB, &d.MemRequestP60KiB,
		&d.MemRequestP95KiB, &d.MemRequestP98KiB, &d.MemRequestP99KiB,
		&d.MemUsageP50KiB, &d.MemUsageP60KiB,
		&d.MemUsageP95KiB, &d.MemUsageP98KiB, &d.MemUsageP99KiB,
		&d.MemUsageMaxKiB,
		&d.MemRSSP95KiB, &d.MemRSSMaxKiB,
		&d.OOMCountSum, &d.CPUUsageMeanMC, &d.MemUsageMeanKiB, &d.SampleCount,
		&d.PodCountMin, &d.PodCountMax, &d.PodCountAvg,
		&d.DesiredReplicas, &d.AvailableReplicas,
		&d.CPUUsageCVBP,
		&ns, &wl, &wlType, &cn,
	); err != nil {
		return types.KeyedDigest{}, fmt.Errorf("pgdigest: scan digest row: %w", err)
	}
	return types.KeyedDigest{
		Key: types.ContainerKey{Namespace: ns, Workload: wl, WorkloadType: wlType, ContainerName: cn},
		Row: d,
	}, nil
}

// ScanKeyedDigestCluster scans one multi-cluster digest row (SelectAllHoursMultiCluster
// / SelectAllHoursForContainers column order: metrics, cpu_usage_cv_bp, cluster_uuid, keys).
func ScanKeyedDigestCluster(rows pgx.Rows) (string, types.KeyedDigest, error) {
	var d types.DigestRow
	var clusterUUID, ns, wl, wlType, cn string
	if err := rows.Scan(
		&d.BucketDate,
		&d.CPURequestP50MC, &d.CPURequestP60MC, &d.CPURequestP95MC, &d.CPURequestP98MC, &d.CPURequestP99MC,
		&d.CPUUsageP50MC, &d.CPUUsageP60MC, &d.CPUUsageP95MC, &d.CPUUsageP98MC, &d.CPUUsageP99MC, &d.CPUUsageMaxMC,
		&d.CPUThrottleP95MC, &d.CPUThrottleMaxMC,
		&d.MemRequestP50KiB, &d.MemRequestP60KiB,
		&d.MemRequestP95KiB, &d.MemRequestP98KiB, &d.MemRequestP99KiB,
		&d.MemUsageP50KiB, &d.MemUsageP60KiB,
		&d.MemUsageP95KiB, &d.MemUsageP98KiB, &d.MemUsageP99KiB,
		&d.MemUsageMaxKiB,
		&d.MemRSSP95KiB, &d.MemRSSMaxKiB,
		&d.OOMCountSum, &d.CPUUsageMeanMC, &d.MemUsageMeanKiB, &d.SampleCount,
		&d.PodCountMin, &d.PodCountMax, &d.PodCountAvg,
		&d.DesiredReplicas, &d.AvailableReplicas,
		&d.CPUUsageCVBP,
		&clusterUUID, &ns, &wl, &wlType, &cn,
	); err != nil {
		return "", types.KeyedDigest{}, fmt.Errorf("pgdigest: scan digest row: %w", err)
	}
	return clusterUUID, types.KeyedDigest{
		Key: types.ContainerKey{Namespace: ns, Workload: wl, WorkloadType: wlType, ContainerName: cn},
		Row: d,
	}, nil
}

// MaxBucketDate returns the latest all_hours bucket_date for this identity.
func MaxBucketDate(ctx context.Context, q Querier, orgID, clusterUUID string) (time.Time, error) {
	if orgID == "" {
		return time.Time{}, fmt.Errorf("pgdigest: org_id is required")
	}
	if clusterUUID == "" {
		return time.Time{}, fmt.Errorf("pgdigest: cluster_uuid is required")
	}
	if q == nil {
		return time.Time{}, fmt.Errorf("pgdigest: querier is required")
	}
	rows, err := q.Query(ctx, `
		SELECT MAX(bucket_date) FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND schedule_type = 'all_hours'`,
		orgID, clusterUUID)
	if err != nil {
		return time.Time{}, fmt.Errorf("pgdigest: max bucket_date: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return time.Time{}, fmt.Errorf("pgdigest: max bucket_date: %w", err)
		}
		return time.Time{}, fmt.Errorf("pgdigest: no digest rows")
	}
	var max *time.Time
	if err := rows.Scan(&max); err != nil {
		return time.Time{}, fmt.Errorf("pgdigest: scan max bucket_date: %w", err)
	}
	if max == nil || max.IsZero() {
		return time.Time{}, fmt.Errorf("pgdigest: no digest rows")
	}
	return max.UTC(), nil
}

// MaxAnyDigestDate returns the latest day across this identity's digest tables
// (container all_hours, namespace usage, node, GPU, PVC, VM, quota, CRQ).
func MaxAnyDigestDate(ctx context.Context, q Querier, orgID, clusterUUID string) (time.Time, error) {
	if err := requireOrgCluster(orgID, clusterUUID); err != nil {
		return time.Time{}, err
	}
	if err := requireQuerier(q); err != nil {
		return time.Time{}, err
	}
	rows, err := q.Query(ctx, `
		SELECT MAX(d) FROM (
			SELECT MAX(bucket_date) AS d FROM daily_container_digests
				WHERE org_id = $1 AND cluster_uuid = $2 AND schedule_type = 'all_hours'
			UNION ALL
			SELECT MAX(bucket_date) FROM daily_namespace_digests
				WHERE org_id = $1 AND cluster_uuid = $2 AND schedule_type = 'all_hours'
			UNION ALL
			SELECT MAX(bucket_date) FROM daily_node_digests
				WHERE org_id = $1 AND cluster_uuid = $2 AND schedule_type = 'all_hours'
			UNION ALL
			SELECT MAX(interval_start::date) FROM gpu_container_digests
				WHERE org_id = $1 AND cluster_uuid = $2 AND schedule_type = 'all_hours'
			UNION ALL
			SELECT MAX(bucket_date) FROM daily_pvc_digests
				WHERE org_id = $1 AND cluster_uuid = $2
			UNION ALL
			SELECT MAX(bucket_date) FROM daily_vm_digests
				WHERE org_id = $1 AND cluster_uuid = $2 AND schedule_type = 'all_hours'
			UNION ALL
			SELECT MAX(report_date) FROM daily_namespace_quota_digests
				WHERE org_id = $1 AND cluster_uuid = $2
			UNION ALL
			SELECT MAX(report_date) FROM daily_cluster_quota_digests
				WHERE org_id = $1 AND cluster_uuid = $2
		) s`,
		orgID, clusterUUID)
	if err != nil {
		return time.Time{}, fmt.Errorf("pgdigest: max digest date: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return time.Time{}, fmt.Errorf("pgdigest: max digest date: %w", err)
		}
		return time.Time{}, fmt.Errorf("pgdigest: no digest rows")
	}
	var max *time.Time
	if err := rows.Scan(&max); err != nil {
		return time.Time{}, fmt.Errorf("pgdigest: scan max digest date: %w", err)
	}
	if max == nil || max.IsZero() {
		return time.Time{}, fmt.Errorf("pgdigest: no digest rows")
	}
	return max.UTC(), nil
}
