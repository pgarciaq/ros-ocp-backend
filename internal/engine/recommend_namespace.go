package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

type namespaceKey struct {
	Namespace string
}

// NamespaceRec combines CPU and memory recommendations for a single namespace
// within a single term and engine.
type NamespaceRec struct {
	OrgID       string
	ClusterUUID string
	Namespace   string
	Term        string
	Engine      string

	RecCPURequestMC  int64
	RecCPULimitMC    int64
	RecMemRequestKiB int64
	RecMemLimitKiB   int64

	CurrentCPURequestMC  int64
	CurrentCPULimitMC    int64
	CurrentMemRequestKiB int64
	CurrentMemLimitKiB   int64

	VariationCPURequestPct float32
	VariationMemRequestPct float32
	ConfidenceLevel        float32
	NotificationCodes      []int16
	DataDays               int
	Stale                  bool
}

// RecommendAllNamespaces reads namespace digests for an org+cluster, groups by
// namespace, computes recommendations for all terms x engines, and returns results.
func RecommendAllNamespaces(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
) ([]NamespaceRec, error) {
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
		ORDER BY namespace, bucket_date`,
		orgID, clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("query namespace digests: %w", err)
	}
	defer rows.Close()

	grouped := map[namespaceKey][]DigestRow{}

	for rows.Next() {
		var d DigestRow
		var ns string

		err := rows.Scan(
			&d.BucketDate,
			&d.CPURequestP50MC, &d.CPURequestP60MC,
			&d.CPURequestP95MC, &d.CPURequestP98MC, &d.CPURequestP99MC,
			&d.CPUUsageP50MC, &d.CPUUsageP60MC,
			&d.CPUUsageP95MC, &d.CPUUsageP98MC, &d.CPUUsageP99MC,
			&d.CPUUsageMaxMC,
			&d.MemRequestP50KiB, &d.MemRequestP60KiB,
			&d.MemRequestP95KiB, &d.MemRequestP98KiB, &d.MemRequestP99KiB,
			&d.MemUsageP50KiB, &d.MemUsageP60KiB,
			&d.MemUsageP95KiB, &d.MemUsageP98KiB, &d.MemUsageP99KiB,
			&d.MemUsageMaxKiB,
			&d.CPUUsageMeanMC, &d.MemUsageMeanKiB,
			&d.SampleCount,
			&ns,
		)
		if err != nil {
			return nil, fmt.Errorf("scan namespace digest row: %w", err)
		}

		key := namespaceKey{Namespace: ns}
		grouped[key] = append(grouped[key], d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate namespace digest rows: %w", err)
	}

	now := time.Now().UTC()
	var results []NamespaceRec

	for key, digestRows := range grouped {
		latest := latestDigest(digestRows)
		currentCPUReqMC := latest.CPURequestP50MC
		currentCPULimMC := latest.CPURequestP95MC
		currentMemReqKiB := latest.MemRequestP50KiB
		currentMemLimKiB := latest.MemRequestP95KiB

		stale := now.Sub(latest.BucketDate.Truncate(24*time.Hour)) > stalenessThreshold

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
				// No OOM at namespace level; memCfg.OOMCountSum stays 0.

				cpuRec := RecommendCPU(windowRows, cpuCfg)
				memRec := RecommendMemory(windowRows, memCfg)

				rec := NamespaceRec{
					OrgID:                orgID,
					ClusterUUID:          clusterUUID,
					Namespace:            key.Namespace,
					Term:                 tc.Name,
					Engine:               profile,
					RecCPURequestMC:      cpuRec.CostRequestMC,
					RecCPULimitMC:        cpuRec.CostLimitMC,
					RecMemRequestKiB:     memRec.CostRequestKiB,
					RecMemLimitKiB:       memRec.CostLimitKiB,
					CurrentCPURequestMC:  currentCPUReqMC,
					CurrentCPULimitMC:    currentCPULimMC,
					CurrentMemRequestKiB: currentMemReqKiB,
					CurrentMemLimitKiB:   currentMemLimKiB,
					ConfidenceLevel:      confidence,
					DataDays:             dataDays,
					Stale:                stale,
				}
				rec.VariationCPURequestPct = computeVariation(currentCPUReqMC, rec.RecCPURequestMC)
				rec.VariationMemRequestPct = computeVariation(currentMemReqKiB, rec.RecMemRequestKiB)
				rec.NotificationCodes = EvaluateNamespaceNotifications(rec)

				results = append(results, rec)
			}
		}
	}

	return results, nil
}

// WriteNamespaceRecommendations batch-upserts NamespaceRec results into
// namespace_recommendation_sets using the native relational columns.
func WriteNamespaceRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []NamespaceRec) error {
	if len(recs) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, r := range recs {
		namespaceID := model.NativeNamespaceID(r.ClusterUUID, r.Namespace)
		batch.Queue(`
			INSERT INTO namespace_recommendation_sets (
				org_id, cluster_uuid, namespace_name,
				term, engine, namespace_id,
				rec_cpu_request_millicores, rec_cpu_limit_millicores,
				rec_memory_request_kib, rec_memory_limit_kib,
				current_cpu_request_millicores, current_cpu_limit_millicores,
				current_memory_request_kib, current_memory_limit_kib,
				variation_cpu_request_pct, variation_memory_request_pct,
				notification_codes, confidence_level, stale,
				monitoring_end_time, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,now(),now())
			ON CONFLICT (org_id, cluster_uuid, namespace_name, term, engine)
			  WHERE term IS NOT NULL
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
				variation_memory_request_pct = EXCLUDED.variation_memory_request_pct,
				notification_codes = EXCLUDED.notification_codes,
				confidence_level = EXCLUDED.confidence_level,
				stale = EXCLUDED.stale,
				namespace_id = EXCLUDED.namespace_id,
				monitoring_end_time = EXCLUDED.monitoring_end_time,
				updated_at = now()`,
			r.OrgID, r.ClusterUUID, r.Namespace,
			r.Term, r.Engine, namespaceID,
			r.RecCPURequestMC, r.RecCPULimitMC,
			r.RecMemRequestKiB, r.RecMemLimitKiB,
			r.CurrentCPURequestMC, r.CurrentCPULimitMC,
			r.CurrentMemRequestKiB, r.CurrentMemLimitKiB,
			r.VariationCPURequestPct, r.VariationMemRequestPct,
			r.NotificationCodes, r.ConfidenceLevel, r.Stale,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for range recs {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("namespace rec batch exec: %w", err)
		}
	}
	return nil
}

// WriteNamespaceRecommendationHistory batch-inserts namespace recommendation
// snapshots into historical_namespace_recommendation_sets using native
// relational columns (no JSONB).
func WriteNamespaceRecommendationHistory(ctx context.Context, pool *pgxpool.Pool, recs []NamespaceRec) error {
	if len(recs) == 0 {
		return nil
	}

	now := time.Now().UTC()
	batch := &pgx.Batch{}

	for _, r := range recs {
		namespaceID := model.NativeNamespaceID(r.ClusterUUID, r.Namespace)
		batch.Queue(`
			INSERT INTO historical_namespace_recommendation_sets (
				org_id, cluster_uuid, namespace_name, namespace_id,
				term, engine,
				rec_cpu_request_millicores, rec_cpu_limit_millicores,
				rec_memory_request_kib, rec_memory_limit_kib,
				current_cpu_request_millicores, current_cpu_limit_millicores,
				current_memory_request_kib, current_memory_limit_kib,
				variation_cpu_request_pct, variation_memory_request_pct,
				notification_codes, confidence_level,
				monitoring_end_time, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$20)
			ON CONFLICT (org_id, cluster_uuid, namespace_name, term, engine, created_at)
			  WHERE term IS NOT NULL
			DO UPDATE SET
				rec_cpu_request_millicores = EXCLUDED.rec_cpu_request_millicores,
				rec_cpu_limit_millicores = EXCLUDED.rec_cpu_limit_millicores,
				rec_memory_request_kib = EXCLUDED.rec_memory_request_kib,
				rec_memory_limit_kib = EXCLUDED.rec_memory_limit_kib,
				notification_codes = EXCLUDED.notification_codes,
				confidence_level = EXCLUDED.confidence_level,
				updated_at = EXCLUDED.updated_at`,
			r.OrgID, r.ClusterUUID, r.Namespace, namespaceID,
			r.Term, r.Engine,
			r.RecCPURequestMC, r.RecCPULimitMC,
			r.RecMemRequestKiB, r.RecMemLimitKiB,
			r.CurrentCPURequestMC, r.CurrentCPULimitMC,
			r.CurrentMemRequestKiB, r.CurrentMemLimitKiB,
			r.VariationCPURequestPct, r.VariationMemRequestPct,
			r.NotificationCodes, r.ConfidenceLevel,
			now, now,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for range recs {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("WriteNamespaceRecommendationHistory batch exec: %w", err)
		}
	}
	return nil
}

// EvaluateNamespaceNotifications produces notification codes for a namespace
// recommendation. Same as container notifications minus OOM and idle detection
// (not applicable at namespace level).
func EvaluateNamespaceNotifications(rec NamespaceRec) []int16 {
	var codes []int16

	if rec.DataDays < 1 {
		codes = append(codes, NotifNewWorkload)
	}
	if rec.ConfidenceLevel < 0.5 && rec.DataDays > 0 {
		codes = append(codes, NotifLowConfidence)
	}

	return codes
}
