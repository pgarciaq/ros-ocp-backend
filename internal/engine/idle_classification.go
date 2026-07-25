package engine

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

// LoadIdleConfig resolves idle detection settings: compiled defaults, env overlay,
// then tenant overrides from recommendation_thresholds (additive exclusions).
func LoadIdleConfig(ctx context.Context, pool *pgxpool.Pool, orgID string) core.IdleConfig {
	settings, err := resolveIdleDetectionSettings(ctx, pool, orgID)
	if err != nil {
		return core.DefaultIdleConfig()
	}
	return idleConfigFromSettings(settings)
}

// AggregateNamespaceIdleState rolls container and GPU idle_state up to namespaces.
// When the aggregated idle_state is idle or zombie, category is also updated to
// match; active namespaces retain their sizing-based category from the engine.
func AggregateNamespaceIdleState(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE namespace_recommendation_sets ns
		SET idle_state = agg.idle_state,
			idle_since = agg.idle_since,
			idle_duration_days = agg.idle_duration_days,
			estimated_waste_cents = agg.estimated_waste_cents,
			category = CASE
				WHEN agg.idle_state IN ('idle', 'zombie') THEN agg.idle_state
				ELSE ns.category
			END
		FROM (
			SELECT
				rs.org_id,
				rs.cluster_uuid,
				rs.namespace,
				CASE
					WHEN bool_or(COALESCE(rs.idle_state, 'active') = 'active')
					  OR bool_or(rs.has_gpu = true AND COALESCE(rs.gpu_idle_state, 'active') = 'active')
						THEN 'active'
					WHEN bool_and(COALESCE(rs.idle_state, 'active') = 'zombie')
					 AND bool_and(NOT rs.has_gpu OR COALESCE(rs.gpu_idle_state, 'active') = 'zombie')
						THEN 'zombie'
					ELSE 'idle'
				END AS idle_state,
				MIN(rs.idle_since) AS idle_since,
				COALESCE(MIN(rs.idle_duration_days), 0) AS idle_duration_days,
				COALESCE(SUM(rs.estimated_waste_cents), 0) AS estimated_waste_cents
			FROM recommendation_sets rs
			WHERE rs.org_id = $1 AND rs.cluster_uuid = $2::uuid
			GROUP BY rs.org_id, rs.cluster_uuid, rs.namespace
		) agg
		WHERE ns.org_id = agg.org_id
		  AND ns.cluster_uuid = agg.cluster_uuid
		  AND ns.namespace_name = agg.namespace
		  AND ns.schedule_type = 'all_hours'`,
		orgID, clusterUUID)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `
		UPDATE namespace_recommendation_sets ns
		SET idle_state = 'active',
			idle_since = NULL,
			idle_duration_days = NULL,
			estimated_waste_cents = 0
		WHERE ns.org_id = $1 AND ns.cluster_uuid = $2::uuid
		  AND ns.schedule_type = 'all_hours'
		  AND NOT EXISTS (
			SELECT 1 FROM recommendation_sets rs
			WHERE rs.org_id = ns.org_id
			  AND rs.cluster_uuid = ns.cluster_uuid
			  AND rs.namespace = ns.namespace_name
		  )`,
		orgID, clusterUUID)
	return err
}
