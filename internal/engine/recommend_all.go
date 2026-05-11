package engine

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

type containerKey struct {
	Namespace     string
	Workload      string
	WorkloadType  string
	ContainerName string
}

// OOMConfig holds configurable OOM bump parameters, typically read from
// environment variables (ROS_OOM_BASE_BUMP, ROS_OOM_MAX_BUMP).
// Zero values cause DefaultMemoryConfig defaults to be used.
type OOMConfig struct {
	BaseBump float64
	MaxBump  float64
}

// RecommendAllWorkloads reads all digests for an org+cluster within [start, end],
// groups them by container, computes recommendations for all terms x 2 engines,
// and returns the results. It does NOT write to the DB.
func RecommendAllWorkloads(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
	oomCfg OOMConfig,
) ([]ContainerRec, error) {
	terms, err := LoadTermConfig(ctx, pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("load term config: %w", err)
	}

	rows, err := pool.Query(ctx, `
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
		ORDER BY namespace, workload, container_name, bucket_date`,
		orgID, clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("query digests: %w", err)
	}
	defer rows.Close()

	grouped := map[containerKey][]DigestRow{}

	for rows.Next() {
		var d DigestRow
		var ns, wl, wlType, cn string

		err := rows.Scan(
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
			&ns, &wl, &wlType, &cn,
		)
		if err != nil {
			return nil, fmt.Errorf("scan digest row: %w", err)
		}

		key := containerKey{Namespace: ns, Workload: wl, WorkloadType: wlType, ContainerName: cn}
		grouped[key] = append(grouped[key], d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate digest rows: %w", err)
	}

	now := time.Now().UTC()
	var results []ContainerRec

	for key, digestRows := range grouped {
		// Current values: use the most recent digest's P50 as the "current" resource config.
		// In a stable deployment, request/limit values are constant across all samples in a day,
		// so P50 == actual current value.
		latest := latestDigest(digestRows)
		currentCPUReqMC := latest.CPURequestP50MC
		currentCPULimMC := latest.CPURequestP95MC // P95 as proxy for limit (which is typically constant or absent)
		currentMemReqKiB := latest.MemRequestP50KiB
		currentMemLimKiB := latest.MemRequestP95KiB

		// Stale detection: if the most recent digest is older than the configured threshold, mark as stale.
		stale := now.Sub(latest.BucketDate.Truncate(24*time.Hour)) > StalenessThreshold()

		for _, tc := range terms {
			windowRows := filterByWindow(digestRows, end, tc.WindowDays)
			if len(windowRows) < tc.MinDataDays {
				continue
			}

			dataDays := len(windowRows)
			confidence := computeConfidence(dataDays, tc.MinDataDays, tc.WindowDays)
			oomTotal := sumOOMCounts(windowRows)
			pcMin, pcMax, pcAvg := aggregatePodCounts(windowRows)
			desiredReplicas, availableReplicas := latestReplicaCounts(windowRows)

			for _, profile := range []string{"cost", "performance"} {
				cpuCfg := cpuConfigForProfile(profile, now, tc.DecayHalfLifeHours)
				memCfg := memConfigForProfile(profile, now, tc.DecayHalfLifeHours)
				memCfg.OOMCountSum = oomTotal
				if oomCfg.BaseBump > 0 {
					memCfg.OOMBaseBump = oomCfg.BaseBump
				}
				if oomCfg.MaxBump > 0 {
					memCfg.OOMMaxBump = oomCfg.MaxBump
				}
				if memCfg.OOMMaxBump < 1.0 {
					memCfg.OOMMaxBump = 1.0
				}

			cpuRec := RecommendCPU(windowRows, cpuCfg)
			memRec := RecommendMemory(windowRows, memCfg)
			abandoned := DetectAbandoned(windowRows)

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

			rec := ContainerRec{
				OrgID:                orgID,
				ClusterUUID:          clusterUUID,
				Namespace:            key.Namespace,
				Workload:             key.Workload,
				WorkloadType:         key.WorkloadType,
				ContainerName:        key.ContainerName,
				Term:                 tc.Name,
				Engine:               profile,
				RecCPURequestMC:      recCPUReq,
				RecCPULimitMC:        recCPULim,
				RecMemRequestKiB:     recMemReq,
				RecMemLimitKiB:       recMemLim,
				CurrentCPURequestMC:  currentCPUReqMC,
				CurrentCPULimitMC:    currentCPULimMC,
				CurrentMemRequestKiB: currentMemReqKiB,
				CurrentMemLimitKiB:   currentMemLimKiB,
				ConfidenceLevel:      confidence,
				CPUTrendSlope:        cpuRec.TrendSlope,
				MemTrendSlope:        memRec.TrendSlope,
				IsIdle:               cpuRec.IsIdle,
				IsAbandoned:          abandoned,
				OOMCountSum:          oomTotal,
				DataDays:             dataDays,
				Stale:                stale,
				PodCountMin:          pcMin,
				PodCountMax:          pcMax,
				PodCountAvg:          pcAvg,
				DesiredReplicas:      desiredReplicas,
				AvailableReplicas:    availableReplicas,
			}
				rec.VariationCPURequestPct = computeVariation(currentCPUReqMC, rec.RecCPURequestMC)
				rec.VariationCPULimitPct = computeVariation(currentCPULimMC, rec.RecCPULimitMC)
				rec.VariationMemRequestPct = computeVariation(currentMemReqKiB, rec.RecMemRequestKiB)
				rec.VariationMemLimitPct = computeVariation(currentMemLimKiB, rec.RecMemLimitKiB)
				rec.NotificationCodes = EvaluateNotifications(rec, tc.MinDataDays)

				results = append(results, rec)
			}
		}
	}

	return results, nil
}

// WriteRecommendations batch-upserts ContainerRec results into recommendation_sets.
func WriteRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []ContainerRec) error {
	if len(recs) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, r := range recs {
		containerID := model.NativeContainerID(r.ClusterUUID, r.Namespace, r.Workload, r.ContainerName)
		batch.Queue(`
			INSERT INTO recommendation_sets (
				org_id, cluster_uuid, namespace, workload, workload_type, container_name,
				term, engine, container_id,
				rec_cpu_request_millicores, rec_cpu_limit_millicores,
				rec_memory_request_kib, rec_memory_limit_kib,
				current_cpu_request_millicores, current_cpu_limit_millicores,
				current_memory_request_kib, current_memory_limit_kib,
				variation_cpu_request_pct, variation_cpu_limit_pct,
				variation_memory_request_pct, variation_memory_limit_pct,
				notification_codes, confidence_level, stale,
				pod_count_min, pod_count_max, pod_count_avg,
				desired_replicas, available_replicas,
				estimated_monthly_savings_usd,
				updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,now())
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, container_name, term, engine)
			DO UPDATE SET
				rec_cpu_request_millicores = EXCLUDED.rec_cpu_request_millicores,
				rec_cpu_limit_millicores = EXCLUDED.rec_cpu_limit_millicores,
				rec_memory_request_kib = EXCLUDED.rec_memory_request_kib,
				rec_memory_limit_kib = EXCLUDED.rec_memory_limit_kib,
				current_cpu_request_millicores = EXCLUDED.current_cpu_request_millicores,
				current_cpu_limit_millicores = EXCLUDED.current_cpu_limit_millicores,
				current_memory_request_kib = EXCLUDED.current_memory_request_kib,
				current_memory_limit_kib = EXCLUDED.current_memory_limit_kib,
				variation_cpu_request_pct = EXCLUDED.variation_cpu_request_pct,
				variation_cpu_limit_pct = EXCLUDED.variation_cpu_limit_pct,
				variation_memory_request_pct = EXCLUDED.variation_memory_request_pct,
				variation_memory_limit_pct = EXCLUDED.variation_memory_limit_pct,
				notification_codes = EXCLUDED.notification_codes,
				confidence_level = EXCLUDED.confidence_level,
				stale = EXCLUDED.stale,
				pod_count_min = EXCLUDED.pod_count_min,
				pod_count_max = EXCLUDED.pod_count_max,
				pod_count_avg = EXCLUDED.pod_count_avg,
				desired_replicas = EXCLUDED.desired_replicas,
				available_replicas = EXCLUDED.available_replicas,
				estimated_monthly_savings_usd = EXCLUDED.estimated_monthly_savings_usd,
				container_id = EXCLUDED.container_id,
				updated_at = now()`,
			r.OrgID, r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.ContainerName,
			r.Term, r.Engine, containerID,
			r.RecCPURequestMC, r.RecCPULimitMC,
			r.RecMemRequestKiB, r.RecMemLimitKiB,
			r.CurrentCPURequestMC, r.CurrentCPULimitMC,
			r.CurrentMemRequestKiB, r.CurrentMemLimitKiB,
			r.VariationCPURequestPct, r.VariationCPULimitPct,
			r.VariationMemRequestPct, r.VariationMemLimitPct,
			r.NotificationCodes, r.ConfidenceLevel, r.Stale,
			r.PodCountMin, r.PodCountMax, r.PodCountAvg,
			r.DesiredReplicas, r.AvailableReplicas,
			r.EstimatedSavingsUSD,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for range recs {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("batch exec: %w", err)
		}
	}
	return nil
}

// filterByWindow returns rows within the last windowDays from endDate (inclusive).
func filterByWindow(rows []DigestRow, endDate time.Time, windowDays int) []DigestRow {
	cutoff := endDate.AddDate(0, 0, -(windowDays - 1))
	var result []DigestRow
	for _, r := range rows {
		d := r.BucketDate.Truncate(24 * time.Hour)
		if !d.Before(cutoff.Truncate(24*time.Hour)) && !d.After(endDate.Truncate(24*time.Hour)) {
			result = append(result, r)
		}
	}
	return result
}

// computeConfidence returns a 0.0-1.0 score based on data availability.
func computeConfidence(dataDays, minDataDays, windowDays int) float32 {
	if dataDays <= 0 {
		return 0
	}
	ratio := float32(dataDays) / float32(windowDays)
	if ratio > 1.0 {
		ratio = 1.0
	}
	return ratio
}

// computeVariation returns the percentage change from current to recommended,
// rounded to the nearest integer.
func computeVariation(current, rec int64) int32 {
	if current == 0 {
		return 0
	}
	return int32(math.Round(float64(rec-current) / float64(current) * 100))
}

func cpuConfigForProfile(profile string, now time.Time, decayHL float64) CPUConfig {
	if profile == "performance" {
		cfg := DefaultCPUConfig(now, decayHL)
		cfg.CostPercentile = 0.98
		cfg.PerfPercentile = 0.98
		return cfg
	}
	return DefaultCPUConfig(now, decayHL)
}

func memConfigForProfile(profile string, now time.Time, decayHL float64) MemoryConfig {
	if profile == "performance" {
		cfg := DefaultMemoryConfig(now, decayHL)
		cfg.CostPercentile = 1.0
		cfg.PerfPercentile = 1.0
		return cfg
	}
	return DefaultMemoryConfig(now, decayHL)
}

// aggregatePodCounts computes min-of-mins, max-of-maxes, and weighted average
// of per-day pod count values across the term window's digest rows.
func aggregatePodCounts(rows []DigestRow) (pcMin, pcMax, pcAvg int64) {
	if len(rows) == 0 {
		return 0, 0, 0
	}
	hasAny := false
	for _, r := range rows {
		if r.PodCountMin > 0 || r.PodCountMax > 0 || r.PodCountAvg > 0 {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return 0, 0, 0
	}
	first := true
	var sumAvg float64
	var count int
	for _, r := range rows {
		if r.PodCountMax == 0 && r.PodCountMin == 0 && r.PodCountAvg == 0 {
			continue
		}
		if first || r.PodCountMin < pcMin {
			pcMin = r.PodCountMin
		}
		if first || r.PodCountMax > pcMax {
			pcMax = r.PodCountMax
		}
		sumAvg += float64(r.PodCountAvg)
		count++
		first = false
	}
	if count > 0 {
		pcAvg = int64(math.Round(sumAvg / float64(count)))
	}
	return
}

// latestReplicaCounts returns the desired and available replica counts from
// the most recent DigestRow that has a non-zero desired_replicas value.
func latestReplicaCounts(rows []DigestRow) (desired, available int64) {
	var latestDate time.Time
	for _, r := range rows {
		if r.DesiredReplicas > 0 && r.BucketDate.After(latestDate) {
			latestDate = r.BucketDate
			desired = r.DesiredReplicas
			available = r.AvailableReplicas
		}
	}
	return desired, available
}

func sumOOMCounts(rows []DigestRow) int64 {
	var total int64
	for _, r := range rows {
		total += r.OOMCountSum
	}
	return total
}

// DefaultStalenessThreshold is used when ROS_STALENESS_THRESHOLD_HOURS is not set.
const DefaultStalenessThreshold = 3 * 24 * time.Hour // 72 hours

// StalenessThreshold returns the configured staleness threshold duration.
func StalenessThreshold() time.Duration {
	cfg := config.GetConfig()
	if cfg.StalenessThresholdHours > 0 {
		return time.Duration(cfg.StalenessThresholdHours) * time.Hour
	}
	return DefaultStalenessThreshold
}

// latestDigest returns the DigestRow with the most recent BucketDate.
func latestDigest(rows []DigestRow) DigestRow {
	if len(rows) == 0 {
		return DigestRow{}
	}
	best := rows[0]
	for _, r := range rows[1:] {
		if r.BucketDate.After(best.BucketDate) {
			best = r
		}
	}
	return best
}
