package engine

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/container"
)

// WriteRecommendationHistory delegates to container/.
var WriteRecommendationHistory = container.WriteRecommendationHistory

// EnsureHistoryPartitions creates monthly partitions for recommendation_history
// covering the current month plus the next 2 months. Idempotent via IF NOT EXISTS.
func EnsureHistoryPartitions(ctx context.Context, pool *pgxpool.Pool) {
	ensureHistoryPartitions(ctx, pool)
}
