package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

const nodeGPUTimeslicingHistoryRetentionDays = 90

// PruneNodeGPUTimeslicingRecommendationHistory deletes history rows older than the retention window.
func PruneNodeGPUTimeslicingRecommendationHistory(ctx context.Context, pool *pgxpool.Pool) error {
	cfg := config.GetConfig()
	days := cfg.HistoryRetentionDays
	if days <= 0 {
		days = nodeGPUTimeslicingHistoryRetentionDays
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	tag, err := pool.Exec(ctx,
		`DELETE FROM node_gpu_timeslicing_recommendation_history WHERE recorded_at < $1`,
		cutoff,
	)
	if err != nil {
		return fmt.Errorf("prune node GPU time-slicing rec history: %w", err)
	}
	if tag.RowsAffected() > 0 {
		logging.GetLogger().Infof(
			"PruneNodeGPUTimeslicingRecommendationHistory: deleted %d rows older than %d days",
			tag.RowsAffected(), days,
		)
	}
	return nil
}
