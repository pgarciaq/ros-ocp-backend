package gpu

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ComputeNodeTimeslicingRecForOrg resolves per-org GPU thresholds (including time-slicing
// parameters) and produces a time-slicing recommendation for a single node × GPU model group.
func ComputeNodeTimeslicingRecForOrg(ctx context.Context, pool *pgxpool.Pool, orgID string, group NodeGPUGroup, gpuRate *float32, now time.Time) *TimeslicingRec {
	settings, err := ResolveGPUThresholdSettings(ctx, pool, orgID)
	if err != nil {
		return ComputeNodeTimeslicingRec(group, gpuRate, now)
	}
	return ComputeNodeTimeslicingRecWithSettings(group, gpuRate, now, settings)
}
