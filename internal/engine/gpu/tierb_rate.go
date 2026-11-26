package gpu

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// ClusterGPUCountMonths returns the cluster's GPU inventory over the window
// in GPU-count-months (#942): Σ node_gpu_count across the daily node digests
// (all_hours schedule) ÷ 30. Missing days simply contribute nothing — the
// spend the rate is divided against comes from the same operator payloads,
// so both sides cover the same observed days.
func ClusterGPUCountMonths(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, start, end time.Time) (float64, error) {
	var gpuDays int64
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(node_gpu_count), 0)
		FROM daily_node_digests
		WHERE org_id = $1
		  AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = 'all_hours'`,
		orgID, clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"),
	).Scan(&gpuDays)
	if err != nil {
		return 0, fmt.Errorf("cluster gpu count months: %w", err)
	}
	return float64(gpuDays) / 30.0, nil
}

// EnsureTierBGPURate derives and deposits the cluster Tier-B GPU rate on
// costData (#942). Called where costData is built with the same window the
// effective_rates fetch used. It is a no-op when costData is nil, when the
// cluster has no GPU inventory or no GPU spend (no rate — current behavior),
// and it never overrides a configured gpu_cost_per_month (GPUMonthlyRate
// reads this field only as the Tier-A fallback).
func EnsureTierBGPURate(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, start, end time.Time, costData *costdata.ClusterCostData) error {
	if costData == nil {
		return nil
	}
	months, err := ClusterGPUCountMonths(ctx, pool, orgID, clusterUUID, start, end)
	if err != nil {
		// The rate is an enhancement, not a gate: a query failure leaves
		// the current behavior (no Tier-B GPU rate) in place.
		return fmt.Errorf("tier-b gpu rate: %w", err)
	}
	costData.DeriveTierBGPURate(months)
	return nil
}
