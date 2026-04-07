package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type containerKey struct {
	Namespace     string
	Workload      string
	WorkloadType  string
	ContainerName string
}

// RecommendAllWorkloads reads all digests for an org+cluster within [start, end],
// groups them by container, computes recommendations for all terms x 2 engines,
// and returns the results. It does NOT write to the DB.
func RecommendAllWorkloads(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
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
			COALESCE(memory_request_p50_kib, 0), COALESCE(memory_request_p95_kib, 0),
			COALESCE(memory_usage_p50_kib, 0), COALESCE(memory_usage_p95_kib, 0), COALESCE(memory_usage_max_kib, 0),
			COALESCE(memory_rss_p95_kib, 0), COALESCE(memory_rss_max_kib, 0),
			COALESCE(oom_count_sum, 0), COALESCE(cpu_usage_mean_mc, 0), COALESCE(memory_usage_mean_kib, 0),
			COALESCE(sample_count, 0),
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
			&d.MemRequestP50KiB, &d.MemRequestP95KiB,
			&d.MemUsageP50KiB, &d.MemUsageP95KiB, &d.MemUsageMaxKiB,
			&d.MemRSSP95KiB, &d.MemRSSMaxKiB,
			&d.OOMCountSum, &d.CPUUsageMeanMC, &d.MemUsageMeanKiB, &d.SampleCount,
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
		for _, tc := range terms {
			windowRows := filterByWindow(digestRows, end, tc.WindowDays)
			if len(windowRows) < tc.MinDataDays {
				continue
			}

			dataDays := len(windowRows)
			confidence := computeConfidence(dataDays, tc.MinDataDays, tc.WindowDays)

			for _, profile := range []string{"cost", "performance"} {
				cpuCfg := cpuConfigForProfile(profile, now, tc.DecayHalfLifeHours)
				memCfg := memConfigForProfile(profile, now, tc.DecayHalfLifeHours)

				cpuRec := RecommendCPU(windowRows, cpuCfg)
				memRec := RecommendMemory(windowRows, memCfg)

				results = append(results, ContainerRec{
					OrgID:            orgID,
					ClusterUUID:      clusterUUID,
					Namespace:        key.Namespace,
					Workload:         key.Workload,
					WorkloadType:     key.WorkloadType,
					ContainerName:    key.ContainerName,
					Term:             tc.Name,
					Engine:           profile,
					RecCPURequestMC:  cpuRec.CostRequestMC,
					RecCPULimitMC:    cpuRec.CostLimitMC,
					RecMemRequestKiB: memRec.CostRequestKiB,
					RecMemLimitKiB:   memRec.CostLimitKiB,
					ConfidenceLevel:  confidence,
					TrendSlope:       cpuRec.TrendSlope,
					IsIdle:           cpuRec.IsIdle,
					OOMCountSum:      sumOOMCounts(windowRows),
					DataDays:         dataDays,
				})
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
		batch.Queue(`
			INSERT INTO recommendation_sets (
				org_id, cluster_uuid, namespace, workload, workload_type, container_name,
				term, engine,
				rec_cpu_request_millicores, rec_cpu_limit_millicores,
				rec_memory_request_kib, rec_memory_limit_kib,
				confidence_level, stale, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,false,now())
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, container_name, term, engine)
			DO UPDATE SET
				rec_cpu_request_millicores = EXCLUDED.rec_cpu_request_millicores,
				rec_cpu_limit_millicores = EXCLUDED.rec_cpu_limit_millicores,
				rec_memory_request_kib = EXCLUDED.rec_memory_request_kib,
				rec_memory_limit_kib = EXCLUDED.rec_memory_limit_kib,
				confidence_level = EXCLUDED.confidence_level,
				stale = false,
				updated_at = now()`,
			r.OrgID, r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.ContainerName,
			r.Term, r.Engine,
			r.RecCPURequestMC, r.RecCPULimitMC,
			r.RecMemRequestKiB, r.RecMemLimitKiB,
			r.ConfidenceLevel,
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

// computeVariation returns the percentage change from current to recommended.
func computeVariation(current, rec int64) float32 {
	if current == 0 {
		return 0
	}
	return float32((float64(rec) - float64(current)) / float64(current) * 100)
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

func sumOOMCounts(rows []DigestRow) int64 {
	var total int64
	for _, r := range rows {
		total += r.OOMCountSum
	}
	return total
}
