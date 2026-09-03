package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SetClusterIngestHooksFailed marks or clears the per-cluster ingest hook degradation flag.
// When failed is true, ingest_hooks_failed_at is set to the current UTC time; when false, it is cleared.
func SetClusterIngestHooksFailed(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, failed bool) error {
	var failedAt *time.Time
	if failed {
		now := time.Now().UTC()
		failedAt = &now
	}
	tag, err := pool.Exec(ctx, `
		UPDATE clusters c
		SET ingest_hooks_failed = $3,
		    ingest_hooks_failed_at = $4
		WHERE c.org_id = $1
		  AND c.cluster_uuid = $2::uuid`,
		orgID, clusterUUID, failed, failedAt,
	)
	if err != nil {
		return fmt.Errorf("SetClusterIngestHooksFailed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return nil
}
