package tags

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NamespaceTags holds resolved tag key/value pairs for all containers in a namespace.
type NamespaceTags struct {
	ClusterUUID string            `json:"cluster_uuid"`
	Namespace   string            `json:"namespace"`
	Tags        map[string]string `json:"tags"`
}

// SyncRequest is the body for POST /internal/tags/sync.
type SyncRequest struct {
	OrgID         string          `json:"org_id"`
	NamespaceTags []NamespaceTags `json:"namespace_tags"`
}

// SyncResponse reports how many org_container_keys rows were updated.
type SyncResponse struct {
	Updated int `json:"updated"`
}

// SyncService upserts resolved tags on org_container_keys.
type SyncService struct {
	pool *pgxpool.Pool
}

// NewSyncService constructs a tag sync service backed by the ROS database pool.
func NewSyncService(pool *pgxpool.Pool) *SyncService {
	return &SyncService{pool: pool}
}

// SyncOrgTags fully replaces resolved_tags for an org, then applies namespace-level tags
// to all matching org_container_keys rows.
func (s *SyncService) SyncOrgTags(ctx context.Context, orgID string, namespaceTags []NamespaceTags) (int, error) {
	if s == nil || s.pool == nil {
		return 0, fmt.Errorf("tag sync service is not configured")
	}
	if orgID == "" {
		return 0, fmt.Errorf("org_id is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tag sync tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE org_container_keys
		SET resolved_tags = '{}'::jsonb
		WHERE org_id = $1`, orgID); err != nil {
		return 0, fmt.Errorf("reset resolved_tags for org %q: %w", orgID, err)
	}

	updated := 0
	for _, nt := range namespaceTags {
		if nt.Namespace == "" || nt.ClusterUUID == "" {
			continue
		}
		tags := nt.Tags
		if tags == nil {
			tags = map[string]string{}
		}
		payload, err := json.Marshal(tags)
		if err != nil {
			return 0, fmt.Errorf("marshal tags for %s/%s: %w", nt.ClusterUUID, nt.Namespace, err)
		}

		tag, err := tx.Exec(ctx, `
			UPDATE org_container_keys
			SET resolved_tags = $4::jsonb
			WHERE org_id = $1
			  AND cluster_uuid = $2::uuid
			  AND namespace = $3`,
			orgID, nt.ClusterUUID, nt.Namespace, string(payload),
		)
		if err != nil {
			return 0, fmt.Errorf("update resolved_tags for %s/%s: %w", nt.ClusterUUID, nt.Namespace, err)
		}
		updated += int(tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit tag sync tx: %w", err)
	}
	return updated, nil
}
