package pgrec

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SourceID is the clusters.source_id value this CLI writes. Any other value
// on any clusters row means the database is not CLI-owned (Helm/Sources).
const SourceID = "robne"

// ErrForeignSourceID is returned when clusters already has a non-robne source_id.
var ErrForeignSourceID = fmt.Errorf("database has clusters.source_id other than %q; refusing write (looks like Helm/Sources)", SourceID)

// AssertCLIOwned refuses the whole write if any clusters row is not source_id=robne.
func AssertCLIOwned(ctx context.Context, pool *pgxpool.Pool) error {
	var n int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM clusters WHERE source_id IS DISTINCT FROM $1`, SourceID).Scan(&n)
	if err != nil {
		return fmt.Errorf("check clusters.source_id: %w", err)
	}
	if n > 0 {
		return ErrForeignSourceID
	}
	return nil
}

// EnsureAccountCluster inserts rh_accounts and clusters for YAML identity.
// cluster_alias is the cluster UUID string (clusters.cluster_alias is NOT NULL).
func EnsureAccountCluster(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, lastReported time.Time) error {
	if lastReported.IsZero() {
		lastReported = time.Now().UTC()
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO rh_accounts (org_id) VALUES ($1)
		ON CONFLICT (org_id) DO NOTHING`, orgID)
	if err != nil {
		return fmt.Errorf("ensure rh_accounts: %w", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, source_id, cluster_uuid, cluster_alias, last_reported_at)
		SELECT ra.id, $2, $3, $4, $5
		FROM rh_accounts ra
		WHERE ra.org_id = $1
		ON CONFLICT (tenant_id, source_id, cluster_uuid, cluster_alias)
		DO UPDATE SET last_reported_at = EXCLUDED.last_reported_at`,
		orgID, SourceID, clusterUUID, clusterUUID, lastReported,
	)
	if err != nil {
		return fmt.Errorf("ensure clusters: %w", err)
	}
	return nil
}
