package engine

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/container"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

// --- Type aliases for backward compat ---

type OldRecommendation = container.OldRecommendation

// --- Function aliases for backward compat ---

var (
	ReadClusterOldRecommendations         = container.ReadClusterOldRecommendations
	ReadOldRecommendations                = container.ReadOldRecommendations
	ReadClusterOldRecommendationsByEngine = container.ReadClusterOldRecommendationsByEngine
	ComputeStabilityPct                   = container.ComputeStabilityPct
	DetectAdoption                        = container.DetectAdoption
	WriteRecommendationQuality            = container.WriteRecommendationQuality
	ContainerKeys                         = container.ContainerKeys
	OOMCountsByContainer                  = container.OOMCountsByContainer
)

// WithinTolerance delegates to core.WithinTolerance.
var WithinTolerance = core.WithinTolerance

// ComputeRecommendationAgeHours delegates to core.
var ComputeRecommendationAgeHours = core.ComputeRecommendationAgeHours

// IsPartitionMissing delegates to core.
var IsPartitionMissing = core.IsPartitionMissing

// EnsureQualityPartitions creates monthly partitions for recommendation_quality
// covering the current month plus the next 2 months. This is idempotent.
func EnsureQualityPartitions(ctx context.Context, pool *pgxpool.Pool) {
	ensureQualityPartitions(ctx, pool)
}
