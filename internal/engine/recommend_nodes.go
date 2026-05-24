package engine

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

// nodeRecsAdvisoryLock is the pg_advisory_xact_lock key shared between
// PersistNodeRecommendations and migration 000058 (PK rebuild) to prevent
// deadlocks without requiring manual worker shutdown during migrations.
const nodeRecsAdvisoryLock = 7358001

// NodeEngineConfig holds per-engine sizing parameters for node recommendations.
type NodeEngineConfig struct {
	Name              string
	TargetUtilization float64
}

// NodeDigestRow represents a single daily digest for a node, loaded from the database.
type NodeDigestRow struct {
	BucketDate        time.Time
	Node              string
	CPUUsageP50MC     int64
	CPUUsageP95MC     int64
	MemUsageP50KiB    int64
	MemUsageP95KiB    int64
	MaxCPUAllocMC     *int64
	MaxMemAllocKiB    *int64
	MaxCPURequestsMC  int64
	MaxMemRequestsKiB int64
	MaxPodCount       int64
	SampleCount       int64
}

// NodeRecConfig holds configuration parameters for the node recommendation engine.
type NodeRecConfig struct {
	UnderutilThreshold         float64
	OvercommitThreshold        float64
	AllocatableFactor          float64
	StrandedImbalanceThreshold float64
	EMAAlpha                   float64
}

// NodeRec holds the computed recommendation for a single node within a single term and engine.
type NodeRec struct {
	Node                       string
	Term                       string
	Engine                     string
	CPUUtilP50                 float32
	CPUUtilP95                 float32
	MemUtilP50                 float32
	MemUtilP95                 float32
	CPUOvercommitRatio         float32
	IsUnderutilized            bool
	IsOvercommitted            bool
	StrandedResource           *string
	PodCount                   int64
	TrendSlope                 float32
	CurrentCPUCores            float64
	CurrentMemoryGiB           float64
	RecommendedCPUCores        float64
	RecommendedMemoryGiB       float64
	NodeCountReduction         int
	EstimatedMonthlySavingsUSD float32
	NotificationCodes          []int16
}

// nodeClassification holds shared utilization signals and flags computed once per (node, term).
type nodeClassification struct {
	Node               string
	PodCount           int64
	validDays          int
	CPUUtilP50         float32
	CPUUtilP95         float32
	MemUtilP50         float32
	MemUtilP95         float32
	CPUOvercommitRatio float32
	IsUnderutilized    bool
	IsOvercommitted    bool
	StrandedResource   *string
	TrendSlope         float32
	CurrentCPUCores    float64
	CurrentMemoryGiB   float64
	NotificationCodes  []int16
	maxCPUUsageP95MC   int64
	maxMemUsageP95KiB  int64
	maxCPURequestsMC   int64
	maxMemRequestsKiB  int64
}

// RecommendNodes evaluates node-level utilization signals from daily digest data.
// It produces one NodeRec per node per term per engine. Shared classification is
// computed once per (node, term); engine-specific sizing and consolidation differ.
func RecommendNodes(digests []NodeDigestRow, cfg NodeRecConfig, nodeSettings NodeThresholdSettings, terms []TermConfig) []NodeRec {
	nodeEngines := NodeEnginesFromThresholds(nodeSettings)
	grouped := map[string][]NodeDigestRow{}
	for _, d := range digests {
		grouped[d.Node] = append(grouped[d.Node], d)
	}

	results := make([]NodeRec, 0, len(grouped)*len(terms)*len(nodeEngines))
	for node, allDays := range grouped {
		latest := latestNodeDigest(allDays)

		for _, tc := range terms {
			windowDays := filterNodeByWindow(allDays, latest.BucketDate, tc.WindowDays)
			if len(windowDays) < tc.MinDataDays {
				continue
			}
			class := classifyNode(node, windowDays, cfg, nodeSettings.TrendMinDays)
			for _, eng := range nodeEngines {
				rec := nodeRecFromClassification(class)
				rec.Term = tc.Name
				rec.Engine = eng.Name
				rec.RecommendedCPUCores, rec.RecommendedMemoryGiB, rec.NodeCountReduction =
					sizeNodeForEngine(class, eng, nodeSettings)
				results = append(results, rec)
			}
		}
	}
	return results
}

func nodeRecFromClassification(class nodeClassification) NodeRec {
	return NodeRec{
		Node:               class.Node,
		PodCount:           class.PodCount,
		CPUUtilP50:         class.CPUUtilP50,
		CPUUtilP95:         class.CPUUtilP95,
		MemUtilP50:         class.MemUtilP50,
		MemUtilP95:         class.MemUtilP95,
		CPUOvercommitRatio: class.CPUOvercommitRatio,
		IsUnderutilized:    class.IsUnderutilized,
		IsOvercommitted:    class.IsOvercommitted,
		StrandedResource:   class.StrandedResource,
		TrendSlope:         class.TrendSlope,
		CurrentCPUCores:    class.CurrentCPUCores,
		CurrentMemoryGiB:   class.CurrentMemoryGiB,
		NotificationCodes:  append([]int16(nil), class.NotificationCodes...),
	}
}

// filterNodeByWindow returns node digest rows within the last windowDays
// from endDate (inclusive), mirroring filterByWindow for container digests.
// Rows are assumed sorted by BucketDate (ascending) from the DB query.
func filterNodeByWindow(rows []NodeDigestRow, endDate time.Time, windowDays int) []NodeDigestRow {
	cutoffDay := endDate.AddDate(0, 0, -(windowDays - 1)).Truncate(24 * time.Hour)
	endDay := endDate.Truncate(24 * time.Hour)

	lo := 0
	hi := len(rows)
	for lo < hi {
		mid := (lo + hi) / 2
		if rows[mid].BucketDate.Truncate(24 * time.Hour).Before(cutoffDay) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	result := make([]NodeDigestRow, 0, len(rows)-lo)
	for i := lo; i < len(rows); i++ {
		d := rows[i].BucketDate.Truncate(24 * time.Hour)
		if d.After(endDay) {
			break
		}
		result = append(result, rows[i])
	}
	return result
}

// latestNodeDigest returns the NodeDigestRow with the most recent BucketDate.
func latestNodeDigest(rows []NodeDigestRow) NodeDigestRow {
	if len(rows) == 0 {
		return NodeDigestRow{}
	}
	best := rows[0]
	for _, r := range rows[1:] {
		if r.BucketDate.After(best.BucketDate) {
			best = r
		}
	}
	return best
}

// classifyNode computes shared utilization classification for a node over a term window.
func classifyNode(node string, days []NodeDigestRow, cfg NodeRecConfig, trendMinDays int) nodeClassification {
	class := nodeClassification{Node: node}

	var (
		cpuUtilSum50      float64
		cpuUtilSum95      float64
		memUtilSum50      float64
		memUtilSum95      float64
		validDays         int
		maxRequests       int64
		maxMemReqs        int64
		maxPodCount       int64
		maxCPUUsageP95MC  int64
		maxMemUsageP95KiB int64
		cpuMeans          []float64
		imbalances        []float64
	)

	for _, d := range days {
		allocCPU := resolveAllocatable(d.MaxCPUAllocMC, d.MaxCPURequestsMC, cfg.AllocatableFactor)
		allocMem := resolveAllocatableMem(d.MaxMemAllocKiB, d.MaxMemRequestsKiB, cfg.AllocatableFactor)

		if allocCPU > 0 && allocMem > 0 {
			cpuUtil50 := float64(d.CPUUsageP50MC) / float64(allocCPU)
			cpuUtil95 := float64(d.CPUUsageP95MC) / float64(allocCPU)
			memUtil50 := float64(d.MemUsageP50KiB) / float64(allocMem)
			memUtil95 := float64(d.MemUsageP95KiB) / float64(allocMem)

			cpuUtilSum50 += cpuUtil50
			cpuUtilSum95 += cpuUtil95
			memUtilSum50 += memUtil50
			memUtilSum95 += memUtil95
			validDays++

			cpuMeans = append(cpuMeans, cpuUtil50)

			high := cpuUtil95
			if memUtil95 > high {
				high = memUtil95
			}
			if high > 1e-9 {
				diff := cpuUtil95 - memUtil95
				if diff < 0 {
					diff = -diff
				}
				imbalances = append(imbalances, diff/high)
			} else {
				imbalances = append(imbalances, 0)
			}
		}

		if d.CPUUsageP95MC > maxCPUUsageP95MC {
			maxCPUUsageP95MC = d.CPUUsageP95MC
		}
		if d.MemUsageP95KiB > maxMemUsageP95KiB {
			maxMemUsageP95KiB = d.MemUsageP95KiB
		}
		if d.MaxCPURequestsMC > maxRequests {
			maxRequests = d.MaxCPURequestsMC
		}
		if d.MaxMemRequestsKiB > maxMemReqs {
			maxMemReqs = d.MaxMemRequestsKiB
		}
		if d.MaxPodCount > maxPodCount {
			maxPodCount = d.MaxPodCount
		}
	}

	class.PodCount = maxPodCount
	class.maxCPUUsageP95MC = maxCPUUsageP95MC
	class.maxMemUsageP95KiB = maxMemUsageP95KiB
	class.maxCPURequestsMC = maxRequests
	class.maxMemRequestsKiB = maxMemReqs
	class.validDays = validDays

	if validDays == 0 {
		return class
	}

	avgCPU50 := cpuUtilSum50 / float64(validDays)
	avgCPU95 := cpuUtilSum95 / float64(validDays)
	avgMem50 := memUtilSum50 / float64(validDays)
	avgMem95 := memUtilSum95 / float64(validDays)

	class.CPUUtilP50 = float32(avgCPU50)
	class.CPUUtilP95 = float32(avgCPU95)
	class.MemUtilP50 = float32(avgMem50)
	class.MemUtilP95 = float32(avgMem95)

	if avgCPU95 < cfg.UnderutilThreshold && avgMem95 < cfg.UnderutilThreshold {
		class.IsUnderutilized = true
		class.NotificationCodes = append(class.NotificationCodes, NotifNodeUnderutilized)
	}

	lastDay := days[len(days)-1]
	allocCPU := resolveAllocatable(lastDay.MaxCPUAllocMC, lastDay.MaxCPURequestsMC, cfg.AllocatableFactor)
	allocMem := resolveAllocatableMem(lastDay.MaxMemAllocKiB, lastDay.MaxMemRequestsKiB, cfg.AllocatableFactor)
	if allocCPU > 0 {
		class.CurrentCPUCores = float64(allocCPU) / 1000.0
	}
	if allocMem > 0 {
		class.CurrentMemoryGiB = float64(allocMem) / (1024.0 * 1024.0)
	}

	if allocCPU > 0 && maxRequests > 0 {
		ratio := float64(maxRequests) / float64(allocCPU)
		class.CPUOvercommitRatio = float32(ratio)
		if ratio > cfg.OvercommitThreshold {
			class.IsOvercommitted = true
			class.NotificationCodes = append(class.NotificationCodes, NotifNodeOvercommitted)
		}
	}

	if len(imbalances) >= 2 {
		imbalanceThresh := cfg.StrandedImbalanceThreshold
		if imbalanceThresh == 0 {
			imbalanceThresh = 0.6
		}
		alpha := cfg.EMAAlpha
		if alpha == 0 {
			alpha = 0.3
		}
		smoothed := emaSmooth(imbalances, alpha)
		finalImbalance := smoothed[len(smoothed)-1]
		if finalImbalance > imbalanceThresh {
			if avgCPU95 > avgMem95 {
				s := "memory"
				class.StrandedResource = &s
			} else {
				s := "cpu"
				class.StrandedResource = &s
			}
			class.NotificationCodes = append(class.NotificationCodes, NotifStrandedResources)
		}
	}

	if len(cpuMeans) >= trendMinDays {
		alpha := cfg.EMAAlpha
		if alpha == 0 {
			alpha = 0.3
		}
		smoothed := emaSmooth(cpuMeans, alpha)
		class.TrendSlope = float32(linearRegressionSlope(smoothed))
	}

	return class
}

// sizeNodeForEngine derives engine-specific recommended capacity and consolidation flag.
func sizeNodeForEngine(class nodeClassification, eng NodeEngineConfig, nodeSettings NodeThresholdSettings) (cpuCores, memGiB float64, nodeCountReduction int) {
	cpuCores, memGiB = recommendedNodeCapacity(
		class.maxCPUUsageP95MC, class.maxMemUsageP95KiB,
		class.maxCPURequestsMC, class.maxMemRequestsKiB,
		eng.TargetUtilization,
	)

	if !class.IsUnderutilized {
		return cpuCores, memGiB, 0
	}

	switch eng.Name {
	case "cost":
		// Cost engine: recommend consolidation when underutilized workloads fit at 80% target.
		nodeCountReduction = 1
	case "performance":
		// Performance engine: only consolidate with extreme waste — workloads fit at target
		// and current capacity has a full spare node worth of headroom.
		if hasFullSpareNodeHeadroom(class.CurrentCPUCores, class.CurrentMemoryGiB, cpuCores, memGiB, nodeSettings.PerfConsolidationHeadroomMultiplier) {
			nodeCountReduction = 1
		}
	}
	return cpuCores, memGiB, nodeCountReduction
}

// hasFullSpareNodeHeadroom reports whether freed capacity could fit another copy of the workload.
func hasFullSpareNodeHeadroom(currentCPU, currentMem, recCPU, recMem, multiplier float64) bool {
	if recCPU <= 0 || recMem <= 0 || currentCPU <= 0 || currentMem <= 0 || multiplier <= 0 {
		return false
	}
	return currentCPU >= multiplier*recCPU && currentMem >= multiplier*recMem
}

// recommendedNodeCapacity derives right-sized CPU cores and memory GiB from peak
// usage and request totals, targeting the given utilization headroom.
func recommendedNodeCapacity(maxCPUUsageP95MC, maxMemUsageP95KiB, maxCPURequestsMC, maxMemRequestsKiB int64, targetUtilization float64) (cpuCores, memGiB float64) {
	var recommendedCPUMC, recommendedMemKiB float64
	if maxCPUUsageP95MC > 0 {
		recommendedCPUMC = float64(maxCPUUsageP95MC) / targetUtilization
	}
	if maxCPURequestsMC > 0 {
		requestBased := float64(maxCPURequestsMC) / targetUtilization
		if requestBased > recommendedCPUMC {
			recommendedCPUMC = requestBased
		}
	}
	if maxMemUsageP95KiB > 0 {
		recommendedMemKiB = float64(maxMemUsageP95KiB) / targetUtilization
	}
	if maxMemRequestsKiB > 0 {
		requestBased := float64(maxMemRequestsKiB) / targetUtilization
		if requestBased > recommendedMemKiB {
			recommendedMemKiB = requestBased
		}
	}
	if recommendedCPUMC > 0 {
		cpuCores = math.Ceil(recommendedCPUMC / 1000.0)
	}
	if recommendedMemKiB > 0 {
		memGiB = math.Ceil(recommendedMemKiB / (1024.0 * 1024.0))
	}
	return cpuCores, memGiB
}

// resolveAllocatable returns the effective allocatable CPU in millicores.
// Prefers the stored allocatable value; falls back to request-based estimate.
func resolveAllocatable(storedAlloc *int64, maxRequests int64, factor float64) int64 {
	if storedAlloc != nil && *storedAlloc > 0 {
		return *storedAlloc
	}
	if maxRequests > 0 {
		return int64(float64(maxRequests) / factor)
	}
	return 0
}

func resolveAllocatableMem(storedAlloc *int64, maxRequests int64, factor float64) int64 {
	if storedAlloc != nil && *storedAlloc > 0 {
		return *storedAlloc
	}
	if maxRequests > 0 {
		return int64(float64(maxRequests) / factor)
	}
	return 0
}

// emaSmooth applies exponential moving average smoothing.
// alpha in (0,1]: higher = less smoothing, lower = more smoothing.
func emaSmooth(ys []float64, alpha float64) []float64 {
	if len(ys) == 0 {
		return ys
	}
	smoothed := make([]float64, len(ys))
	smoothed[0] = ys[0]
	for i := 1; i < len(ys); i++ {
		smoothed[i] = alpha*ys[i] + (1-alpha)*smoothed[i-1]
	}
	return smoothed
}

// linearRegressionSlope computes the slope of a simple OLS linear regression
// over equally-spaced points (index as X, value as Y).
func linearRegressionSlope(ys []float64) float64 {
	n := float64(len(ys))
	if n < 2 {
		return 0
	}
	var sumX, sumY, sumXY, sumX2 float64
	for i, y := range ys {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}
	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}

// QueryNodeDigests reads daily_node_digests for a cluster within a time range.
func QueryNodeDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, start, end time.Time) ([]NodeDigestRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT bucket_date, node,
			COALESCE(cpu_usage_p50_mc, 0), COALESCE(cpu_usage_p95_mc, 0),
			COALESCE(mem_usage_p50_kib, 0), COALESCE(mem_usage_p95_kib, 0),
			max_cpu_allocatable_mc, max_mem_allocatable_kib,
			COALESCE(max_cpu_requests_mc, 0), COALESCE(max_mem_requests_kib, 0),
			COALESCE(max_pod_count, 0), COALESCE(sample_count, 0)
		FROM daily_node_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date <= $4
		ORDER BY node, bucket_date`,
		orgID, clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	// N.B. filterNodeByWindow uses binary search and relies on bucket_date sort order above.
	if err != nil {
		return nil, fmt.Errorf("query node digests: %w", err)
	}
	defer rows.Close()

	var result []NodeDigestRow
	for rows.Next() {
		var d NodeDigestRow
		err := rows.Scan(
			&d.BucketDate, &d.Node,
			&d.CPUUsageP50MC, &d.CPUUsageP95MC,
			&d.MemUsageP50KiB, &d.MemUsageP95KiB,
			&d.MaxCPUAllocMC, &d.MaxMemAllocKiB,
			&d.MaxCPURequestsMC, &d.MaxMemRequestsKiB,
			&d.MaxPodCount, &d.SampleCount,
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

// PersistNodeRecommendations upserts computed node recommendations into the database.
func PersistNodeRecommendations(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, recs []NodeRec, validTerms []string) error {
	if len(recs) == 0 {
		return nil
	}

	t0 := time.Now()
	defer func() { metrics.ObserveDB("persist_node_recommendations", t0) }()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Advisory lock serializes with migration 000058 (PK rebuild).
	// If the migration is running, this blocks until it completes rather than deadlocking.
	if _, err := tx.Exec(ctx, fmt.Sprintf("SELECT pg_advisory_xact_lock(%d)", nodeRecsAdvisoryLock)); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}

	for _, r := range recs {
		_, err := tx.Exec(ctx, `
			INSERT INTO node_recommendations (
				org_id, cluster_uuid, node, term, engine,
				cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
				cpu_overcommit_ratio, is_underutilized, is_overcommitted,
				stranded_resource, pod_count, trend_slope, notification_codes,
				recommended_cpu_cores, recommended_memory_gib, node_count_reduction,
				estimated_monthly_savings_usd,
				updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,now())
			ON CONFLICT (org_id, cluster_uuid, node, term, engine) DO UPDATE SET
				cpu_util_p50 = EXCLUDED.cpu_util_p50,
				cpu_util_p95 = EXCLUDED.cpu_util_p95,
				mem_util_p50 = EXCLUDED.mem_util_p50,
				mem_util_p95 = EXCLUDED.mem_util_p95,
				cpu_overcommit_ratio = EXCLUDED.cpu_overcommit_ratio,
				is_underutilized = EXCLUDED.is_underutilized,
				is_overcommitted = EXCLUDED.is_overcommitted,
				stranded_resource = EXCLUDED.stranded_resource,
				pod_count = EXCLUDED.pod_count,
				trend_slope = EXCLUDED.trend_slope,
				notification_codes = EXCLUDED.notification_codes,
				recommended_cpu_cores = EXCLUDED.recommended_cpu_cores,
				recommended_memory_gib = EXCLUDED.recommended_memory_gib,
				node_count_reduction = EXCLUDED.node_count_reduction,
				estimated_monthly_savings_usd = EXCLUDED.estimated_monthly_savings_usd,
				updated_at = now()`,
			orgID, clusterUUID, r.Node, r.Term, r.Engine,
			r.CPUUtilP50, r.CPUUtilP95, r.MemUtilP50, r.MemUtilP95,
			r.CPUOvercommitRatio, r.IsUnderutilized, r.IsOvercommitted,
			r.StrandedResource, r.PodCount, r.TrendSlope, r.NotificationCodes,
			r.RecommendedCPUCores, r.RecommendedMemoryGiB, r.NodeCountReduction,
			r.EstimatedMonthlySavingsUSD,
		)
		if err != nil {
			return fmt.Errorf("upsert node rec %s: %w", r.Node, err)
		}
	}

	// Remove rows for terms no longer in the active config (stale term cleanup).
	if len(validTerms) > 0 {
		_, err = tx.Exec(ctx, `
			DELETE FROM node_recommendations
			WHERE org_id = $1 AND cluster_uuid = $2
			  AND term != ALL($3)`,
			orgID, clusterUUID, validTerms,
		)
		if err != nil {
			return fmt.Errorf("cleanup stale terms: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit node recs: %w", err)
	}

	logging.ForOrg(orgID, clusterUUID).Infof("PersistNodeRecommendations: upserted %d recs", len(recs))
	return nil
}
