package engine

import (
	"context"
	"time"

	libengine "github.com/redhatinsights/ros-ocp-backend/librobne/engine"
)

// DefaultEngineConfig builds a pool-free config from compiled defaults.
// Tests and the compute-only canary use this; production wrappers overlay
// tenant terms, thresholds, and idle settings loaded from the database.
func DefaultEngineConfig(orgID, clusterUUID string, now time.Time) EngineConfig {
	cfg := libengine.DefaultEngineConfig(orgID, clusterUUID, now)
	cfg.Terms = DefaultTerms()
	cfg.Sizing = DefaultContainerSizingThresholds()
	cfg.Idle = DefaultIdleConfig()
	return cfg
}

// RecommendWorkloads runs the container recommendation loop with no pool.
// rows must be ordered by container key then BucketDate (same as the digest SELECT).
// emit is called every BatchSize containers (default 500); the batch backing
// array is reused — copy if retaining. ApplySavingsEstimates is a separate call.
func RecommendWorkloads(ctx context.Context, rows []KeyedDigest, cfg EngineConfig, emit EmitContainer) error {
	return libengine.RecommendWorkloads(ctx, rows, cfg, emit)
}
