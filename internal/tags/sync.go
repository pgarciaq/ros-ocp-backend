package tags

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ContainerTags holds resolved tag key/value pairs for one container identity.
type ContainerTags struct {
	ClusterUUID   string            `json:"cluster_uuid"`
	Namespace     string            `json:"namespace"`
	Workload      string            `json:"workload"`
	ContainerName string            `json:"container_name"`
	Tags          map[string]string `json:"tags"`
}

// SyncRequest is the body for POST /internal/tags/sync.
type SyncRequest struct {
	OrgID          string          `json:"org_id"`
	ContainerTags  []ContainerTags `json:"container_tags"`
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

// SyncOrgTags updates resolved_tags for existing org_container_keys rows that match
// each container identity in containerTags. Rows that do not exist are skipped.
func (s *SyncService) SyncOrgTags(ctx context.Context, orgID string, containerTags []ContainerTags) (int, error) {
	if s == nil || s.pool == nil {
		return 0, fmt.Errorf("tag sync service is not configured")
	}
	if orgID == "" {
		return 0, fmt.Errorf("org_id is required")
	}
	if len(containerTags) == 0 {
		return 0, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tag sync tx: %w", err)
	}
	defer tx.Rollback(ctx)

	updated := 0
	for _, ct := range containerTags {
		if ct.Namespace == "" || ct.Workload == "" || ct.ContainerName == "" {
			continue
		}
		if ct.ClusterUUID == "" {
			continue
		}
		tags := ct.Tags
		if tags == nil {
			tags = map[string]string{}
		}
		payload, err := json.Marshal(tags)
		if err != nil {
			return 0, fmt.Errorf("marshal tags for %s/%s/%s: %w", ct.Namespace, ct.Workload, ct.ContainerName, err)
		}

		tag, err := tx.Exec(ctx, `
			UPDATE org_container_keys
			SET resolved_tags = $6::jsonb
			WHERE org_id = $1
			  AND cluster_uuid = $2::uuid
			  AND namespace = $3
			  AND workload = $4
			  AND container_name = $5`,
			orgID, ct.ClusterUUID, ct.Namespace, ct.Workload, ct.ContainerName, string(payload),
		)
		if err != nil {
			return 0, fmt.Errorf("update resolved_tags for %s/%s/%s: %w", ct.Namespace, ct.Workload, ct.ContainerName, err)
		}
		updated += int(tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit tag sync tx: %w", err)
	}
	return updated, nil
}
