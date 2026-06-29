package model

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OrgNamespaceKey mirrors org_namespace_keys for ORM reads when needed.
type OrgNamespaceKey struct {
	OrgID         string `gorm:"column:org_id"`
	ClusterUUID   string `gorm:"column:cluster_uuid"`
	NamespaceName string `gorm:"column:namespace_name"`
}

func (OrgNamespaceKey) TableName() string {
	return "org_namespace_keys"
}

// RefreshOrgNamespaceKeys upserts active namespace keys and removes stale entries.
func RefreshOrgNamespaceKeys(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	if orgID == "" {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for org namespace keys: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := RefreshOrgNamespaceKeysTx(ctx, tx, orgID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RefreshOrgNamespaceKeysTx upserts active namespace keys within an existing transaction.
func RefreshOrgNamespaceKeysTx(ctx context.Context, tx pgx.Tx, orgID string) error {
	if orgID == "" {
		return nil
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO org_namespace_keys (
			org_id, cluster_uuid, namespace_name, last_reported
		)
		SELECT org_id, cluster_uuid, namespace_name, MAX(updated_at) AS last_reported
		FROM namespace_recommendation_sets
		WHERE org_id = $1
		  AND term IS NOT NULL
		  AND stale = false
		GROUP BY org_id, cluster_uuid, namespace_name
		ON CONFLICT (org_id, cluster_uuid, namespace_name) DO UPDATE SET
			last_reported = EXCLUDED.last_reported`,
		orgID,
	)
	if err != nil {
		return fmt.Errorf("upsert org namespace keys: %w", err)
	}

	_, err = tx.Exec(ctx, `
		DELETE FROM org_namespace_keys onk
		WHERE onk.org_id = $1
		  AND NOT EXISTS (
			SELECT 1
			FROM namespace_recommendation_sets nrs
			WHERE nrs.org_id = onk.org_id
			  AND nrs.cluster_uuid = onk.cluster_uuid
			  AND nrs.namespace_name = onk.namespace_name
			  AND nrs.term IS NOT NULL
			  AND nrs.stale = false
		  )`,
		orgID,
	)
	if err != nil {
		return fmt.Errorf("delete stale org namespace keys: %w", err)
	}
	return nil
}
