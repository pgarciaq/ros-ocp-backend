package bhschedule

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PruneClusterBusinessHoursDigests removes all business_hours digest rows for a cluster.
func PruneClusterBusinessHoursDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	if _, err := pool.Exec(ctx, `
		DELETE FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND schedule_type = 'business_hours'`,
		orgID, clusterUUID); err != nil {
		return fmt.Errorf("prune container business_hours digests: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM daily_namespace_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND schedule_type = 'business_hours'`,
		orgID, clusterUUID); err != nil {
		return fmt.Errorf("prune namespace business_hours digests: %w", err)
	}
	return nil
}

// PruneNamespaceBusinessHoursDigests removes business_hours digest rows for one namespace.
func PruneNamespaceBusinessHoursDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, namespace string) error {
	if _, err := pool.Exec(ctx, `
		DELETE FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3 AND schedule_type = 'business_hours'`,
		orgID, clusterUUID, namespace); err != nil {
		return fmt.Errorf("prune container business_hours digests for namespace: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM daily_namespace_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3 AND schedule_type = 'business_hours'`,
		orgID, clusterUUID, namespace); err != nil {
		return fmt.Errorf("prune namespace business_hours digests for namespace: %w", err)
	}
	return nil
}
