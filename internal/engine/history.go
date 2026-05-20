package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
)

// WriteRecommendationHistory batch-inserts recommendation snapshots into
// recommendation_history for trend analysis and audit. Each ContainerRec
// produces one row keyed by (container, term, engine, recorded_at).
func WriteRecommendationHistory(ctx context.Context, pool *pgxpool.Pool, recs []ContainerRec, sourceBinary string) error {
	if len(recs) == 0 {
		return nil
	}

	// Bucket to UTC midnight so re-processing the same calendar day upserts instead of
	// multiplying history rows (#62).
	nowClock := time.Now().UTC()
	recordedAt := time.Date(nowClock.Year(), nowClock.Month(), nowClock.Day(), 0, 0, 0, 0, time.UTC)
	batch := &pgx.Batch{}

	for _, r := range recs {
		batch.Queue(`
			INSERT INTO recommendation_history (
				recorded_at, org_id, cluster_uuid, namespace, workload, workload_type, container_name,
				term, engine,
				rec_cpu_request_millicores, rec_cpu_limit_millicores,
				rec_memory_request_kib, rec_memory_limit_kib,
				notification_codes, confidence_level,
				estimated_monthly_savings_usd, source_binary
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, recorded_at)
			DO UPDATE SET
				rec_cpu_request_millicores = EXCLUDED.rec_cpu_request_millicores,
				rec_cpu_limit_millicores = EXCLUDED.rec_cpu_limit_millicores,
				rec_memory_request_kib = EXCLUDED.rec_memory_request_kib,
				rec_memory_limit_kib = EXCLUDED.rec_memory_limit_kib,
				notification_codes = EXCLUDED.notification_codes,
				confidence_level = EXCLUDED.confidence_level,
				estimated_monthly_savings_usd = EXCLUDED.estimated_monthly_savings_usd,
				source_binary = EXCLUDED.source_binary`,
			recordedAt, r.OrgID, r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.ContainerName,
			r.Term, r.Engine,
			r.RecCPURequestMC, r.RecCPULimitMC,
			r.RecMemRequestKiB, r.RecMemLimitKiB,
			r.NotificationCodes, r.ConfidenceLevel,
			r.EstimatedSavingsUSD, sourceBinary,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for range recs {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("WriteRecommendationHistory batch exec: %w", err)
		}
	}
	return nil
}

// EnsureHistoryPartitions creates monthly partitions for recommendation_history
// covering the current month plus the next 2 months. Idempotent via IF NOT EXISTS.
func EnsureHistoryPartitions(ctx context.Context, pool *pgxpool.Pool) {
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, i, 0)
		monthEnd := monthStart.AddDate(0, 1, 0)
		partName := fmt.Sprintf("recommendation_history_%s", monthStart.Format("200601"))

		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF recommendation_history FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			monthStart.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, sql); err != nil {
			log.Warnf("EnsureHistoryPartitions: %s: %v (non-fatal)", partName, err)
		}
	}
}
