package pgrec

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RefreshOrgMetadata updates org_container_keys and org_recommendation_stats.
// Cache invalidation stays in the processor wrapper (internal/engine).
func RefreshOrgMetadata(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	if orgID == "" {
		return nil
	}
	if err := RefreshOrgContainerKeys(ctx, pool, orgID); err != nil {
		return err
	}
	return RefreshOrgRecommendationStats(ctx, pool, orgID)
}

// RefreshOrgContainerKeys upserts active container keys and removes stale entries.
func RefreshOrgContainerKeys(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	if orgID == "" {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for org container keys: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := RefreshOrgContainerKeysTx(ctx, tx, orgID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RefreshOrgContainerKeysTx upserts container keys inside an existing transaction.
func RefreshOrgContainerKeysTx(ctx context.Context, tx pgx.Tx, orgID string) error {
	if orgID == "" {
		return nil
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO org_container_keys (
			org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, last_reported, is_stale
		)
		SELECT
			org_id,
			cluster_uuid,
			namespace,
			workload,
			workload_type,
			container_name,
			last_reported,
			is_stale
		FROM (
			SELECT DISTINCT ON (org_id, namespace, workload, container_name)
				org_id,
				cluster_uuid,
				namespace,
				workload,
				workload_type,
				container_name,
				updated_at AS last_reported,
				stale AS is_stale
			FROM recommendation_sets
			WHERE org_id = $1
			ORDER BY org_id, namespace, workload, container_name, updated_at DESC
		) latest
		ON CONFLICT (org_id, namespace, workload, container_name) DO UPDATE SET
			cluster_uuid = EXCLUDED.cluster_uuid,
			last_reported = EXCLUDED.last_reported,
			workload_type = EXCLUDED.workload_type,
			is_stale = EXCLUDED.is_stale`,
		orgID,
	)
	if err != nil {
		return fmt.Errorf("upsert org container keys: %w", err)
	}

	_, err = tx.Exec(ctx, `
		DELETE FROM org_container_keys ock
		WHERE ock.org_id = $1
		  AND NOT EXISTS (
			SELECT 1
			FROM recommendation_sets rs
			WHERE rs.org_id = ock.org_id
			  AND rs.namespace = ock.namespace
			  AND rs.workload = ock.workload
			  AND rs.container_name = ock.container_name
		  )`,
		orgID,
	)
	if err != nil {
		return fmt.Errorf("delete orphan org container keys: %w", err)
	}
	return nil
}

// RefreshOrgRecommendationStats recomputes and upserts org list counts.
func RefreshOrgRecommendationStats(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	if orgID == "" {
		return nil
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO org_recommendation_stats (org_id, container_count, namespace_count, updated_at)
		SELECT
			$1,
			COUNT(DISTINCT (namespace, workload, container_name)),
			COUNT(DISTINCT namespace),
			NOW()
		FROM recommendation_sets
		WHERE org_id = $1 AND stale = false
		ON CONFLICT (org_id) DO UPDATE SET
			container_count = EXCLUDED.container_count,
			namespace_count = EXCLUDED.namespace_count,
			updated_at = EXCLUDED.updated_at`,
		orgID,
	)
	if err != nil {
		return fmt.Errorf("refresh org recommendation stats: %w", err)
	}
	return nil
}

// RefreshOrgRecommendationStatsTx is like RefreshOrgRecommendationStats but uses an existing tx.
func RefreshOrgRecommendationStatsTx(ctx context.Context, tx pgx.Tx, orgID string) error {
	if orgID == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO org_recommendation_stats (org_id, container_count, namespace_count, updated_at)
		SELECT
			$1,
			COUNT(DISTINCT (namespace, workload, container_name)),
			COUNT(DISTINCT namespace),
			NOW()
		FROM recommendation_sets
		WHERE org_id = $1 AND stale = false
		ON CONFLICT (org_id) DO UPDATE SET
			container_count = EXCLUDED.container_count,
			namespace_count = EXCLUDED.namespace_count,
			updated_at = EXCLUDED.updated_at`,
		orgID,
	)
	if err != nil {
		return fmt.Errorf("refresh org recommendation stats: %w", err)
	}
	return nil
}
