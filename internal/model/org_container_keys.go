package model

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OrgContainerKey mirrors org_container_keys for ORM reads when needed.
type OrgContainerKey struct {
	OrgID         string `gorm:"column:org_id"`
	ClusterUUID   string `gorm:"column:cluster_uuid"`
	Namespace     string `gorm:"column:namespace"`
	Workload      string `gorm:"column:workload"`
	WorkloadType  string `gorm:"column:workload_type"`
	ContainerName string `gorm:"column:container_name"`
	IsStale       bool   `gorm:"column:is_stale"`
}

func (OrgContainerKey) TableName() string {
	return "org_container_keys"
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
	defer tx.Rollback(ctx)

	if err := RefreshOrgContainerKeysTx(ctx, tx, orgID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RefreshOrgContainerKeysTx upserts all container keys (stale and non-stale) within
// an existing transaction. The is_stale column tracks whether the most-recently-updated
// recommendation_sets row for each container composite key is stale.
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
