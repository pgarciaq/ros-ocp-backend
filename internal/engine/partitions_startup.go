package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// EnsureRecommendationPartitionsAtStartup pre-creates monthly partitions for
// recommendation_history, recommendation_quality, pvc_recommendation_quality,
// vm_recommendation_quality, gpu_mig_recommendation_quality, and
// snapshot_recommendation_quality (current + next 2 months).
func EnsureRecommendationPartitionsAtStartup(ctx context.Context, pool *pgxpool.Pool) {
	ensureHistoryPartitions(ctx, pool)
	ensureQualityPartitions(ctx, pool)
	ensureEntityQualityPartitions(ctx, pool, "pvc_recommendation_quality")
	ensureEntityQualityPartitions(ctx, pool, "vm_recommendation_quality")
	ensureEntityQualityPartitions(ctx, pool, "gpu_mig_recommendation_quality")
	ensureEntityQualityPartitions(ctx, pool, "snapshot_recommendation_quality")
}

func ensureHistoryPartitions(ctx context.Context, pool *pgxpool.Pool) {
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
			logging.GetLogger().Warnf("EnsureHistoryPartitions: %s: %v (non-fatal)", partName, err)
		}
	}
}

func ensureQualityPartitions(ctx context.Context, pool *pgxpool.Pool) {
	ensureEntityQualityPartitions(ctx, pool, "recommendation_quality")
}

// ensureEntityQualityPartitions creates monthly partitions for the given
// quality table (current + next 2 months). Idempotent.
// New partitions get autovacuum_analyze_scale_factor=0.05 to match migration 000171.
func ensureEntityQualityPartitions(ctx context.Context, pool *pgxpool.Pool, tableName string) {
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, i, 0)
		monthEnd := monthStart.AddDate(0, 1, 0)
		partName := fmt.Sprintf("%s_%s", tableName, monthStart.Format("200601"))

		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')`,
			partName, tableName,
			monthStart.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, sql); err != nil {
			logging.GetLogger().Warnf("ensureEntityQualityPartitions(%s): %s: %v (non-fatal)", tableName, partName, err)
			continue
		}
		relopts := fmt.Sprintf(
			`ALTER TABLE %s SET (autovacuum_analyze_scale_factor = 0.05)`,
			partName,
		)
		if _, err := pool.Exec(ctx, relopts); err != nil {
			logging.GetLogger().Warnf("ensureEntityQualityPartitions(%s): %s reloptions: %v (non-fatal)", tableName, partName, err)
		}
	}
}
