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

// QueryContainerDigestsByScheduleType loads digest rows for a cluster and schedule stream.
func QueryContainerDigestsByScheduleType(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
	scheduleType string,
) (map[containerKey][]DigestRow, error) {
	rows, err := pool.Query(ctx, containerDigestSelect+`
		ORDER BY namespace, workload, workload_type, container_name, bucket_date`,
		orgID, clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"), scheduleType,
	)
	if err != nil {
		return nil, fmt.Errorf("query container digests schedule_type=%s: %w", scheduleType, err)
	}
	defer rows.Close()

	grouped := make(map[containerKey][]DigestRow)
	for rows.Next() {
		var d DigestRow
		var ns, wl, wlType, cn string
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
			&ns, &wl, &wlType, &cn,
		); err != nil {
			return nil, fmt.Errorf("scan container digest: %w", err)
		}
		key := containerKey{Namespace: ns, Workload: wl, WorkloadType: wlType, ContainerName: cn}
		grouped[key] = append(grouped[key], d)
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
	for i := range results {
		byCluster[results[i].ClusterUUID] = append(byCluster[results[i].ClusterUUID], i)
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

	for clusterUUID, indices := range byCluster {
		start, end, err := digestWindowForCluster(ctx, pool, orgID, clusterUUID, "daily_container_digests", maxTermWindowDays(terms))
		if err != nil {
			return err
		}
		cache, err := LoadSchedules(ctx, pool, orgID, clusterUUID)
		if err != nil {
			return fmt.Errorf("load business hours schedules: %w", err)
		}
		if !cache.HasAnyEnabled() {
			continue
		}

		bhDigests, err := QueryContainerDigestsByScheduleType(ctx, pool, orgID, clusterUUID, start, end, digestScheduleBusinessHours)
		if err != nil {
			return err
		}

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
	var maxDate time.Time
	q := fmt.Sprintf(`
		SELECT COALESCE(MAX(bucket_date), CURRENT_DATE)
		FROM %s
		WHERE org_id = $1 AND cluster_uuid = $2`, table)
	if err := pool.QueryRow(ctx, q, orgID, clusterUUID).Scan(&maxDate); err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("max digest date for %s: %w", table, err)
	}
	end = maxDate.Truncate(24 * time.Hour)
	start = end.AddDate(0, 0, -(windowDays - 1))
	return start, end, nil
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

func queryNamespaceDigestsByScheduleType(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
	scheduleType string,
) (map[namespaceKey][]DigestRow, error) {
	rows, err := pool.Query(ctx, namespaceDigestSelect+`
		ORDER BY namespace, bucket_date`,
		orgID, clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"), scheduleType,
	)
	if err != nil {
		return nil, fmt.Errorf("query namespace digests schedule_type=%s: %w", scheduleType, err)
	}
	defer rows.Close()

	grouped := make(map[namespaceKey][]DigestRow)
	for rows.Next() {
		var d DigestRow
		var ns string
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
			&ns,
		); err != nil {
			return nil, fmt.Errorf("scan namespace digest: %w", err)
		}
		key := namespaceKey{Namespace: ns}
		grouped[key] = append(grouped[key], d)
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
	for i := range results {
		byCluster[results[i].ClusterUUID] = append(byCluster[results[i].ClusterUUID], i)
	}

	terms, err := LoadTermConfigCached(ctx, pool, orgID, "namespace")
	if err != nil {
		return fmt.Errorf("load namespace term config for business hours: %w", err)
	}

	sizingThresholds, err := ResolveNamespaceSizingThresholds(ctx, pool, orgID)
	if err != nil {
		return fmt.Errorf("load namespace thresholds for business hours: %w", err)
	}

	for clusterUUID, indices := range byCluster {
		start, end, err := digestWindowForCluster(ctx, pool, orgID, clusterUUID, "daily_namespace_digests", maxTermWindowDays(terms))
		if err != nil {
			return err
		}
		cache, err := LoadSchedules(ctx, pool, orgID, clusterUUID)
		if err != nil {
			return fmt.Errorf("load business hours schedules: %w", err)
		}
		if !cache.HasAnyEnabled() {
			continue
		}

		bhDigests, err := queryNamespaceDigestsByScheduleType(ctx, pool, orgID, clusterUUID, start, end, digestScheduleBusinessHours)
		if err != nil {
			return err
		}

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
