package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

const (
	digestScheduleAllHours      = "all_hours"
	digestScheduleBusinessHours = "business_hours"
)

// BusinessHoursEngineResult holds a business-hours perspective for one engine, or a reason when data is insufficient.
type BusinessHoursEngineResult struct {
	CPURequestMillicores *int64
	CPULimitMillicores   *int64
	MemRequestKiB        *int64
	MemLimitKiB          *int64
	Reason               string
}

const containerDigestSelect = `
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
			namespace, workload, workload_type, container_name
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = $5`

const containerDigestSelectMultiCluster = `
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
			cluster_uuid::text, namespace, workload, workload_type, container_name
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = ANY($2::uuid[])
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = $5`

// QueryContainerDigestsByScheduleType loads digest rows for a cluster and schedule stream.
func QueryContainerDigestsByScheduleType(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
	scheduleType string,
) (map[containerKey][]DigestRow, error) {
	byCluster, err := QueryContainerDigestsByScheduleTypeForClusters(ctx, pool, orgID, []string{clusterUUID}, start, end, scheduleType)
	if err != nil {
		return nil, err
	}
	return byCluster[clusterUUID], nil
}

// QueryContainerDigestsByScheduleTypeForClusters loads digest rows for multiple clusters in one query.
func QueryContainerDigestsByScheduleTypeForClusters(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	clusterUUIDs []string,
	start, end time.Time,
	scheduleType string,
) (map[string]map[containerKey][]DigestRow, error) {
	if len(clusterUUIDs) == 0 {
		return map[string]map[containerKey][]DigestRow{}, nil
	}

	rows, err := pool.Query(ctx, containerDigestSelectMultiCluster+`
		ORDER BY cluster_uuid, namespace, workload, workload_type, container_name, bucket_date`,
		orgID, clusterUUIDs, start.Format("2006-01-02"), end.Format("2006-01-02"), scheduleType,
	)
	if err != nil {
		return nil, fmt.Errorf("query container digests schedule_type=%s: %w", scheduleType, err)
	}
	defer rows.Close()

	grouped := make(map[string]map[containerKey][]DigestRow)
	for rows.Next() {
		var d DigestRow
		var clusterUUID, ns, wl, wlType, cn string
		if err := rows.Scan(
			&d.BucketDate,
			&d.CPURequestP50MC, &d.CPURequestP60MC,
			&d.CPURequestP95MC, &d.CPURequestP98MC, &d.CPURequestP99MC,
			&d.CPUUsageP50MC, &d.CPUUsageP60MC,
			&d.CPUUsageP95MC, &d.CPUUsageP98MC, &d.CPUUsageP99MC, &d.CPUUsageMaxMC,
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
			&clusterUUID, &ns, &wl, &wlType, &cn,
		); err != nil {
			return nil, fmt.Errorf("scan container digest: %w", err)
		}
		key := containerKey{Namespace: ns, Workload: wl, WorkloadType: wlType, ContainerName: cn}
		if grouped[clusterUUID] == nil {
			grouped[clusterUUID] = make(map[containerKey][]DigestRow)
		}
		grouped[clusterUUID][key] = append(grouped[clusterUUID][key], d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate container digests: %w", err)
	}
	return grouped, nil
}

func recommendContainerStream(
	key containerKey,
	digests []DigestRow,
	terms []TermConfig,
	oomCfg OOMConfig,
	sizingThresholds SizingThresholdSettings,
) map[string]map[string]BusinessHoursEngineResult {
	if len(digests) == 0 {
		return nil
	}

	now := time.Now().UTC()
	latest := latestDigest(digests)
	out := make(map[string]map[string]BusinessHoursEngineResult)

	for _, tc := range terms {
		windowRows := filterByWindow(digests, latest.BucketDate, tc.WindowDays)
		termKey := tc.Name + "_term"

		for _, profile := range []string{"cost", "performance"} {
			if len(windowRows) == 0 {
				continue
			}
			if len(windowRows) < tc.MinDataDays {
				if out[termKey] == nil {
					out[termKey] = make(map[string]BusinessHoursEngineResult)
				}
				out[termKey][profile] = BusinessHoursEngineResult{
					Reason: insufficientBusinessHoursReason(len(windowRows), tc.MinDataDays),
				}
				continue
			}

			oomTotal := sumOOMCounts(windowRows)
			cpuCfg := cpuConfigForProfile(profile, now, tc.DecayHalfLifeHours, sizingThresholds)
			memCfg := memConfigForProfile(profile, now, tc.DecayHalfLifeHours, sizingThresholds, oomCfg)
			memCfg.OOMCountSum = oomTotal
			if memCfg.OOMMaxBump < 1.0 {
				memCfg.OOMMaxBump = 1.0
			}

			cpuRec := RecommendCPU(windowRows, cpuCfg)
			memRec := RecommendMemory(windowRows, memCfg)

			var recCPUReq, recCPULim, recMemReq, recMemLim int64
			if profile == "performance" {
				recCPUReq = cpuRec.PerfRequestMC
				recCPULim = cpuRec.PerfLimitMC
				recMemReq = memRec.PerfRequestKiB
				recMemLim = memRec.PerfLimitKiB
			} else {
				recCPUReq = cpuRec.CostRequestMC
				recCPULim = cpuRec.CostLimitMC
				recMemReq = memRec.CostRequestKiB
				recMemLim = memRec.CostLimitKiB
			}

			if out[termKey] == nil {
				out[termKey] = make(map[string]BusinessHoursEngineResult)
			}
			out[termKey][profile] = BusinessHoursEngineResult{
				CPURequestMillicores: int64Ptr(recCPUReq),
				CPULimitMillicores:   int64Ptr(recCPULim),
				MemRequestKiB:        int64Ptr(recMemReq),
				MemLimitKiB:          int64Ptr(recMemLim),
			}
		}
	}
	return out
}

func insufficientBusinessHoursReason(dataDays, minDataDays int) string {
	return fmt.Sprintf("insufficient business hours data: %d days available, %d required", dataDays, minDataDays)
}

func int64Ptr(v int64) *int64 {
	return &v
}

// EnrichNativeContainerResultsWithBusinessHours attaches optional business_hours fields to API results.
func EnrichNativeContainerResultsWithBusinessHours(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	results []model.NativeContainerResult,
) error {
	if !config.BusinessHoursFeatureEnabled() || pool == nil || len(results) == 0 {
		return nil
	}

	byCluster := make(map[string][]int)
	clusterUUIDs := make([]string, 0)
	seenCluster := make(map[string]bool)
	for i := range results {
		cu := results[i].ClusterUUID
		byCluster[cu] = append(byCluster[cu], i)
		if !seenCluster[cu] {
			seenCluster[cu] = true
			clusterUUIDs = append(clusterUUIDs, cu)
		}
	}

	terms, err := LoadTermConfigCached(ctx, pool, orgID, "container")
	if err != nil {
		return fmt.Errorf("load term config for business hours: %w", err)
	}

	sizingThresholds, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	if err != nil {
		return fmt.Errorf("load container thresholds for business hours: %w", err)
	}

	oomCfg := OOMConfig{}
	windowDays := maxTermWindowDays(terms)

	scheduleCaches, err := LoadSchedulesForClusters(ctx, pool, orgID, clusterUUIDs)
	if err != nil {
		return fmt.Errorf("load business hours schedules: %w", err)
	}

	digestWindows, err := digestWindowsForClusters(ctx, pool, orgID, "daily_container_digests", clusterUUIDs, windowDays)
	if err != nil {
		return err
	}
	queryStart, queryEnd := mergeDigestQueryWindow(digestWindows)

	bhDigestsByCluster, err := QueryContainerDigestsByScheduleTypeForClusters(
		ctx, pool, orgID, clusterUUIDs, queryStart, queryEnd, digestScheduleBusinessHours,
	)
	if err != nil {
		return err
	}

	for clusterUUID, indices := range byCluster {
		cache := scheduleCaches[clusterUUID]
		if cache == nil || !cache.HasAnyEnabled() {
			continue
		}
		bhDigests := bhDigestsByCluster[clusterUUID]

		for _, idx := range indices {
			r := &results[idx]
			sched := cache.Resolve(r.Project)
			if !sched.Enabled {
				continue
			}
			key := containerKey{
				Namespace:     r.Project,
				Workload:      r.Workload,
				WorkloadType:  r.WorkloadType,
				ContainerName: r.Container,
			}
			digests := bhDigests[key]
			if len(digests) == 0 {
				continue
			}
			bhByTerm := recommendContainerStream(key, digests, terms, oomCfg, sizingThresholds)
			attachBusinessHoursToContainerTerms(r, bhByTerm)
		}
	}
	return nil
}

func attachBusinessHoursToContainerTerms(r *model.NativeContainerResult, bhByTerm map[string]map[string]BusinessHoursEngineResult) {
	if len(bhByTerm) == 0 {
		return
	}
	for termKey, termRec := range r.Recommendations {
		engines, ok := bhByTerm[termKey]
		if !ok {
			continue
		}
		if bh, ok := engines["cost"]; ok {
			if termRec.Cost != nil {
				termRec.Cost.BusinessHours = businessHoursToModel(bh)
			}
		}
		if bh, ok := engines["performance"]; ok {
			if termRec.Performance != nil {
				termRec.Performance.BusinessHours = businessHoursToModel(bh)
			}
		}
		r.Recommendations[termKey] = termRec
	}
}

func businessHoursToModel(bh BusinessHoursEngineResult) *model.BusinessHoursRecommendation {
	if bh.Reason != "" {
		return &model.BusinessHoursRecommendation{Reason: bh.Reason}
	}
	return &model.BusinessHoursRecommendation{
		CPURequestMillicores: bh.CPURequestMillicores,
		CPULimitMillicores:   bh.CPULimitMillicores,
		MemRequestKiB:        bh.MemRequestKiB,
		MemLimitKiB:          bh.MemLimitKiB,
	}
}

func maxTermWindowDays(terms []TermConfig) int {
	max := 1
	for _, tc := range terms {
		if tc.WindowDays > max {
			max = tc.WindowDays
		}
	}
	return max
}

func digestWindowForCluster(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, table string,
	windowDays int,
) (start, end time.Time, err error) {
	windows, err := digestWindowsForClusters(ctx, pool, orgID, table, []string{clusterUUID}, windowDays)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	w := windows[clusterUUID]
	return w.start, w.end, nil
}

type digestWindow struct {
	start time.Time
	end   time.Time
}

func digestWindowsForClusters(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, table string,
	clusterUUIDs []string,
	windowDays int,
) (map[string]digestWindow, error) {
	out := make(map[string]digestWindow, len(clusterUUIDs))
	if len(clusterUUIDs) == 0 {
		return out, nil
	}
	q := fmt.Sprintf(`
		SELECT cluster_uuid::text, COALESCE(MAX(bucket_date), CURRENT_DATE)
		FROM %s
		WHERE org_id = $1 AND cluster_uuid = ANY($2::uuid[])
		GROUP BY cluster_uuid`, table)
	rows, err := pool.Query(ctx, q, orgID, clusterUUIDs)
	if err != nil {
		return nil, fmt.Errorf("max digest dates for %s: %w", table, err)
	}
	defer rows.Close()

	seen := make(map[string]bool, len(clusterUUIDs))
	for rows.Next() {
		var clusterUUID string
		var maxDate time.Time
		if err := rows.Scan(&clusterUUID, &maxDate); err != nil {
			return nil, fmt.Errorf("scan max digest date: %w", err)
		}
		end := maxDate.Truncate(24 * time.Hour)
		start := end.AddDate(0, 0, -(windowDays - 1))
		out[clusterUUID] = digestWindow{start: start, end: end}
		seen[clusterUUID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Clusters with no digest rows still get a window anchored on today.
	now := time.Now().UTC().Truncate(24 * time.Hour)
	for _, clusterUUID := range clusterUUIDs {
		if seen[clusterUUID] {
			continue
		}
		out[clusterUUID] = digestWindow{
			start: now.AddDate(0, 0, -(windowDays - 1)),
			end:   now,
		}
	}
	return out, nil
}

func mergeDigestQueryWindow(windows map[string]digestWindow) (start, end time.Time) {
	first := true
	for _, w := range windows {
		if first {
			start, end = w.start, w.end
			first = false
			continue
		}
		if w.start.Before(start) {
			start = w.start
		}
		if w.end.After(end) {
			end = w.end
		}
	}
	return start, end
}

const namespaceDigestSelect = `
		SELECT bucket_date,
			COALESCE(cpu_request_p50_mc, 0), COALESCE(cpu_request_p60_mc, 0),
			COALESCE(cpu_request_p95_mc, 0), COALESCE(cpu_request_p98_mc, 0), COALESCE(cpu_request_p99_mc, 0),
			COALESCE(cpu_usage_p50_mc, 0), COALESCE(cpu_usage_p60_mc, 0),
			COALESCE(cpu_usage_p95_mc, 0), COALESCE(cpu_usage_p98_mc, 0), COALESCE(cpu_usage_p99_mc, 0),
			COALESCE(cpu_usage_max_mc, 0),
			COALESCE(memory_request_p50_kib, 0), COALESCE(memory_request_p60_kib, 0),
			COALESCE(memory_request_p95_kib, 0), COALESCE(memory_request_p98_kib, 0), COALESCE(memory_request_p99_kib, 0),
			COALESCE(memory_usage_p50_kib, 0), COALESCE(memory_usage_p60_kib, 0),
			COALESCE(memory_usage_p95_kib, 0), COALESCE(memory_usage_p98_kib, 0), COALESCE(memory_usage_p99_kib, 0),
			COALESCE(memory_usage_max_kib, 0),
			COALESCE(cpu_usage_mean_mc, 0), COALESCE(memory_usage_mean_kib, 0),
			COALESCE(sample_count, 0),
			namespace
		FROM daily_namespace_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = $5`

const namespaceDigestSelectMultiCluster = `
		SELECT bucket_date,
			COALESCE(cpu_request_p50_mc, 0), COALESCE(cpu_request_p60_mc, 0),
			COALESCE(cpu_request_p95_mc, 0), COALESCE(cpu_request_p98_mc, 0), COALESCE(cpu_request_p99_mc, 0),
			COALESCE(cpu_usage_p50_mc, 0), COALESCE(cpu_usage_p60_mc, 0),
			COALESCE(cpu_usage_p95_mc, 0), COALESCE(cpu_usage_p98_mc, 0), COALESCE(cpu_usage_p99_mc, 0),
			COALESCE(cpu_usage_max_mc, 0),
			COALESCE(memory_request_p50_kib, 0), COALESCE(memory_request_p60_kib, 0),
			COALESCE(memory_request_p95_kib, 0), COALESCE(memory_request_p98_kib, 0), COALESCE(memory_request_p99_kib, 0),
			COALESCE(memory_usage_p50_kib, 0), COALESCE(memory_usage_p60_kib, 0),
			COALESCE(memory_usage_p95_kib, 0), COALESCE(memory_usage_p98_kib, 0), COALESCE(memory_usage_p99_kib, 0),
			COALESCE(memory_usage_max_kib, 0),
			COALESCE(cpu_usage_mean_mc, 0), COALESCE(memory_usage_mean_kib, 0),
			COALESCE(sample_count, 0),
			cluster_uuid::text, namespace
		FROM daily_namespace_digests
		WHERE org_id = $1 AND cluster_uuid = ANY($2::uuid[])
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = $5`

func queryNamespaceDigestsByScheduleType(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
	scheduleType string,
) (map[namespaceKey][]DigestRow, error) {
	byCluster, err := queryNamespaceDigestsByScheduleTypeForClusters(ctx, pool, orgID, []string{clusterUUID}, start, end, scheduleType)
	if err != nil {
		return nil, err
	}
	return byCluster[clusterUUID], nil
}

func queryNamespaceDigestsByScheduleTypeForClusters(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	clusterUUIDs []string,
	start, end time.Time,
	scheduleType string,
) (map[string]map[namespaceKey][]DigestRow, error) {
	if len(clusterUUIDs) == 0 {
		return map[string]map[namespaceKey][]DigestRow{}, nil
	}
	rows, err := pool.Query(ctx, namespaceDigestSelectMultiCluster+`
		ORDER BY cluster_uuid, namespace, bucket_date`,
		orgID, clusterUUIDs, start.Format("2006-01-02"), end.Format("2006-01-02"), scheduleType,
	)
	if err != nil {
		return nil, fmt.Errorf("query namespace digests schedule_type=%s: %w", scheduleType, err)
	}
	defer rows.Close()

	grouped := make(map[string]map[namespaceKey][]DigestRow)
	for rows.Next() {
		var d DigestRow
		var clusterUUID, ns string
		if err := rows.Scan(
			&d.BucketDate,
			&d.CPURequestP50MC, &d.CPURequestP60MC,
			&d.CPURequestP95MC, &d.CPURequestP98MC, &d.CPURequestP99MC,
			&d.CPUUsageP50MC, &d.CPUUsageP60MC,
			&d.CPUUsageP95MC, &d.CPUUsageP98MC, &d.CPUUsageP99MC, &d.CPUUsageMaxMC,
			&d.MemRequestP50KiB, &d.MemRequestP60KiB,
			&d.MemRequestP95KiB, &d.MemRequestP98KiB, &d.MemRequestP99KiB,
			&d.MemUsageP50KiB, &d.MemUsageP60KiB,
			&d.MemUsageP95KiB, &d.MemUsageP98KiB, &d.MemUsageP99KiB,
			&d.MemUsageMaxKiB,
			&d.CPUUsageMeanMC, &d.MemUsageMeanKiB,
			&d.SampleCount,
			&clusterUUID, &ns,
		); err != nil {
			return nil, fmt.Errorf("scan namespace digest: %w", err)
		}
		key := namespaceKey{Namespace: ns}
		if grouped[clusterUUID] == nil {
			grouped[clusterUUID] = make(map[namespaceKey][]DigestRow)
		}
		grouped[clusterUUID][key] = append(grouped[clusterUUID][key], d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate namespace digests: %w", err)
	}
	return grouped, nil
}

func recommendNamespaceStream(digests []DigestRow, terms []TermConfig, sizingThresholds SizingThresholdSettings) map[string]map[string]BusinessHoursEngineResult {
	if len(digests) == 0 {
		return nil
	}
	now := time.Now().UTC()
	latest := latestDigest(digests)
	out := make(map[string]map[string]BusinessHoursEngineResult)

	for _, tc := range terms {
		windowRows := filterByWindow(digests, latest.BucketDate, tc.WindowDays)
		termKey := tc.Name + "_term"

		for _, profile := range []string{"cost", "performance"} {
			if len(windowRows) == 0 {
				continue
			}
			if len(windowRows) < tc.MinDataDays {
				if out[termKey] == nil {
					out[termKey] = make(map[string]BusinessHoursEngineResult)
				}
				out[termKey][profile] = BusinessHoursEngineResult{
					Reason: insufficientBusinessHoursReason(len(windowRows), tc.MinDataDays),
				}
				continue
			}

			cpuCfg := cpuConfigForProfile(profile, now, tc.DecayHalfLifeHours, sizingThresholds)
			memCfg := memConfigForProfile(profile, now, tc.DecayHalfLifeHours, sizingThresholds, OOMConfig{})
			cpuRec := RecommendCPU(windowRows, cpuCfg)
			memRec := RecommendMemory(windowRows, memCfg)

			var recCPUReq, recCPULim, recMemReq, recMemLim int64
			if profile == "performance" {
				recCPUReq = cpuRec.PerfRequestMC
				recCPULim = cpuRec.PerfLimitMC
				recMemReq = memRec.PerfRequestKiB
				recMemLim = memRec.PerfLimitKiB
			} else {
				recCPUReq = cpuRec.CostRequestMC
				recCPULim = cpuRec.CostLimitMC
				recMemReq = memRec.CostRequestKiB
				recMemLim = memRec.CostLimitKiB
			}

			if out[termKey] == nil {
				out[termKey] = make(map[string]BusinessHoursEngineResult)
			}
			out[termKey][profile] = BusinessHoursEngineResult{
				CPURequestMillicores: int64Ptr(recCPUReq),
				CPULimitMillicores:   int64Ptr(recCPULim),
				MemRequestKiB:        int64Ptr(recMemReq),
				MemLimitKiB:          int64Ptr(recMemLim),
			}
		}
	}
	return out
}

// EnrichNativeNamespaceResultsWithBusinessHours attaches optional business_hours fields to namespace API results.
func EnrichNativeNamespaceResultsWithBusinessHours(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	results []model.NativeNamespaceResult,
) error {
	if !config.BusinessHoursFeatureEnabled() || pool == nil || len(results) == 0 {
		return nil
	}

	byCluster := make(map[string][]int)
	clusterUUIDs := make([]string, 0)
	seenCluster := make(map[string]bool)
	for i := range results {
		cu := results[i].ClusterUUID
		byCluster[cu] = append(byCluster[cu], i)
		if !seenCluster[cu] {
			seenCluster[cu] = true
			clusterUUIDs = append(clusterUUIDs, cu)
		}
	}

	terms, err := LoadTermConfigCached(ctx, pool, orgID, "namespace")
	if err != nil {
		return fmt.Errorf("load namespace term config for business hours: %w", err)
	}

	sizingThresholds, err := ResolveNamespaceSizingThresholds(ctx, pool, orgID)
	if err != nil {
		return fmt.Errorf("load namespace thresholds for business hours: %w", err)
	}

	windowDays := maxTermWindowDays(terms)
	scheduleCaches, err := LoadSchedulesForClusters(ctx, pool, orgID, clusterUUIDs)
	if err != nil {
		return fmt.Errorf("load business hours schedules: %w", err)
	}

	digestWindows, err := digestWindowsForClusters(ctx, pool, orgID, "daily_namespace_digests", clusterUUIDs, windowDays)
	if err != nil {
		return err
	}
	queryStart, queryEnd := mergeDigestQueryWindow(digestWindows)

	bhDigestsByCluster, err := queryNamespaceDigestsByScheduleTypeForClusters(
		ctx, pool, orgID, clusterUUIDs, queryStart, queryEnd, digestScheduleBusinessHours,
	)
	if err != nil {
		return err
	}

	for clusterUUID, indices := range byCluster {
		cache := scheduleCaches[clusterUUID]
		if cache == nil || !cache.HasAnyEnabled() {
			continue
		}
		bhDigests := bhDigestsByCluster[clusterUUID]

		for _, idx := range indices {
			r := &results[idx]
			sched := cache.Resolve(r.Project)
			if !sched.Enabled {
				continue
			}
			key := namespaceKey{Namespace: r.Project}
			digests := bhDigests[key]
			if len(digests) == 0 {
				continue
			}
			bhByTerm := recommendNamespaceStream(digests, terms, sizingThresholds)
			attachBusinessHoursToNamespaceTerms(r, bhByTerm)
		}
	}
	return nil
}

func attachBusinessHoursToNamespaceTerms(r *model.NativeNamespaceResult, bhByTerm map[string]map[string]BusinessHoursEngineResult) {
	if len(bhByTerm) == 0 {
		return
	}
	for termKey, raw := range r.Recommendations {
		if termKey == "monitoring_end_time" {
			continue
		}
		termRec, ok := raw.(model.TermRecommendation)
		if !ok {
			continue
		}
		engines, ok := bhByTerm[termKey]
		if !ok {
			continue
		}
		if bh, ok := engines["cost"]; ok && termRec.Cost != nil {
			termRec.Cost.BusinessHours = businessHoursToModel(bh)
		}
		if bh, ok := engines["performance"]; ok && termRec.Performance != nil {
			termRec.Performance.BusinessHours = businessHoursToModel(bh)
		}
		r.Recommendations[termKey] = termRec
	}
}
