package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SetClusterAnalyticsIncomplete marks or clears the per-cluster analytics staleness flag.
// When incomplete is true, analytics_incomplete_at is set to the current UTC time; when false, it is cleared.
func SetClusterAnalyticsIncomplete(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, incomplete bool) error {
	var incompleteAt *time.Time
	if incomplete {
		now := time.Now().UTC()
		incompleteAt = &now
	}
	tag, err := pool.Exec(ctx, `
		UPDATE clusters c
		SET analytics_incomplete = $3,
		    analytics_incomplete_at = $4
		WHERE c.org_id = $1
		  AND c.cluster_uuid = $2::uuid`,
		orgID, clusterUUID, incomplete, incompleteAt,
	)
	if err != nil {
		return fmt.Errorf("SetClusterAnalyticsIncomplete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return nil
}
