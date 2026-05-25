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

// RefreshOrgContainerKeysTx upserts active container keys within an existing transaction.
func RefreshOrgContainerKeysTx(ctx context.Context, tx pgx.Tx, orgID string) error {
	if orgID == "" {
		return nil
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO org_container_keys (
			org_id, cluster_uuid, namespace, workload, workload_type, container_name, last_reported
		)
		SELECT
			org_id,
			cluster_uuid,
			namespace,
			workload,
			workload_type,
			container_name,
			last_reported
		FROM (
			SELECT DISTINCT ON (org_id, namespace, workload, container_name)
				org_id,
				cluster_uuid,
				namespace,
				workload,
				workload_type,
				container_name,
				updated_at AS last_reported
			FROM recommendation_sets
			WHERE org_id = $1 AND stale = false
			ORDER BY org_id, namespace, workload, container_name, updated_at DESC
		) active
		ON CONFLICT (org_id, namespace, workload, container_name) DO UPDATE SET
			cluster_uuid = EXCLUDED.cluster_uuid,
			last_reported = EXCLUDED.last_reported,
			workload_type = EXCLUDED.workload_type`,
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
			  AND rs.stale = false
		  )`,
		orgID,
	)
	if err != nil {
		return fmt.Errorf("delete stale org container keys: %w", err)
	}
	return nil
}
