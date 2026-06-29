package tags

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TagKeyCatalog lists an enabled tag key and its observed values for the current period.
type TagKeyCatalog struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

// NamespaceTags holds resolved tag key/value pairs for all containers in a namespace.
type NamespaceTags struct {
	ClusterUUID string            `json:"cluster_uuid"`
	Namespace   string            `json:"namespace"`
	Tags        map[string]string `json:"tags"`
}

// SyncRequest is the body for POST /internal/tags/sync.
type SyncRequest struct {
	OrgID         string          `json:"org_id"`
	SyncedAt      string          `json:"synced_at"`
	TagKeys       []TagKeyCatalog `json:"tag_keys"`
	NamespaceTags []NamespaceTags `json:"namespace_tags"`
}

// SyncResponse reports how many org_container_keys rows were updated.
type SyncResponse struct {
	Updated int `json:"updated"`
}

// SyncStatus reports org-level tag catalog and freshness.
type SyncStatus struct {
	OrgID    string          `json:"org_id"`
	Source   string          `json:"source,omitempty"`
	Note     string          `json:"note,omitempty"`
	SyncedAt *time.Time      `json:"synced_at,omitempty"`
	TagKeys  []TagKeyCatalog `json:"tag_keys"`
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
func (s *SyncService) SyncOrgTags(ctx context.Context, req SyncRequest) (int, error) {
	if s == nil || s.pool == nil {
		return 0, fmt.Errorf("tag sync service is not configured")
	}
	orgID := strings.TrimSpace(req.OrgID)
	if orgID == "" {
		return 0, fmt.Errorf("org_id is required")
	}

	tagKeys := normalizeTagKeys(req.TagKeys)
	syncedAt, err := parseSyncedAt(req.SyncedAt)
	if err != nil {
		return 0, err
	}

	previousKeys, _ := s.loadPreviousTagKeys(ctx, orgID)
	logRemovedTagKeys(orgID, previousKeys, tagKeys)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tag sync tx: %w", err)
	}
	defer tx.Rollback(ctx)

	validNamespaceTags := make([]NamespaceTags, 0, len(req.NamespaceTags))
	for _, nt := range req.NamespaceTags {
		if nt.Namespace == "" || nt.ClusterUUID == "" {
			continue
		}
		validNamespaceTags = append(validNamespaceTags, nt)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE org_container_keys
		SET resolved_tags = '{}'::jsonb
		WHERE org_id = $1`, orgID); err != nil {
		return 0, fmt.Errorf("reset resolved_tags for org %q: %w", orgID, err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE org_namespace_keys
		SET resolved_tags = '{}'::jsonb
		WHERE org_id = $1`, orgID); err != nil {
		return 0, fmt.Errorf("reset namespace resolved_tags for org %q: %w", orgID, err)
	}

	updated := 0
	if len(validNamespaceTags) > 0 {
		clusterUUIDs := make([]string, len(validNamespaceTags))
		namespaces := make([]string, len(validNamespaceTags))
		tagsPayloads := make([]string, len(validNamespaceTags))
		for i, nt := range validNamespaceTags {
			tags := normalizeNamespaceTags(nt.Tags)
			payload, err := json.Marshal(tags)
			if err != nil {
				return 0, fmt.Errorf("marshal tags for %s/%s: %w", nt.ClusterUUID, nt.Namespace, err)
			}
			clusterUUIDs[i] = nt.ClusterUUID
			namespaces[i] = nt.Namespace
			tagsPayloads[i] = string(payload)
		}

		tag, err := tx.Exec(ctx, `
			UPDATE org_container_keys AS o
			SET resolved_tags = v.tags::jsonb
			FROM (
				SELECT u.c, u.n, u.t AS tags
				FROM unnest($2::uuid[], $3::text[], $4::text[]) AS u(c, n, t)
			) AS v
			WHERE o.org_id = $1
			  AND o.cluster_uuid = v.c
			  AND o.namespace = v.n`,
			orgID, clusterUUIDs, namespaces, tagsPayloads,
		)
		if err != nil {
			return 0, fmt.Errorf("batch update resolved_tags for org %q: %w", orgID, err)
		}
		updated = int(tag.RowsAffected())

		nsTag, err := tx.Exec(ctx, `
			UPDATE org_namespace_keys AS o
			SET resolved_tags = v.tags::jsonb
			FROM (
				SELECT u.c, u.n, u.t AS tags
				FROM unnest($2::uuid[], $3::text[], $4::text[]) AS u(c, n, t)
			) AS v
			WHERE o.org_id = $1
			  AND o.cluster_uuid = v.c
			  AND o.namespace_name = v.n`,
			orgID, clusterUUIDs, namespaces, tagsPayloads,
		)
		if err != nil {
			return 0, fmt.Errorf("batch update namespace resolved_tags for org %q: %w", orgID, err)
		}
		updated += int(nsTag.RowsAffected())
	}

	tagKeysPayload, err := json.Marshal(tagKeys)
	if err != nil {
		return 0, fmt.Errorf("marshal tag_keys for org %q: %w", orgID, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO org_tag_sync_metadata (org_id, synced_at, tag_keys, updated_at)
		VALUES ($1, $2, $3::jsonb, NOW())
		ON CONFLICT (org_id) DO UPDATE SET
			synced_at = EXCLUDED.synced_at,
			tag_keys = EXCLUDED.tag_keys,
			updated_at = NOW()`,
		orgID, syncedAt, string(tagKeysPayload),
	); err != nil {
		return 0, fmt.Errorf("upsert tag sync metadata for org %q: %w", orgID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit tag sync tx: %w", err)
	}
	return updated, nil
}

// GetSyncStatus returns per-org tag sync metadata.
func (s *SyncService) GetSyncStatus(ctx context.Context, orgID string) (*SyncStatus, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("tag sync service is not configured")
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, fmt.Errorf("org_id is required")
	}

	var syncedAt time.Time
	var tagKeysRaw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT synced_at, tag_keys
		FROM org_tag_sync_metadata
		WHERE org_id = $1`, orgID,
	).Scan(&syncedAt, &tagKeysRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return &SyncStatus{OrgID: orgID, TagKeys: []TagKeyCatalog{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load tag sync metadata for org %q: %w", orgID, err)
	}

	tagKeys, err := decodeTagKeys(tagKeysRaw)
	if err != nil {
		return nil, fmt.Errorf("decode tag_keys for org %q: %w", orgID, err)
	}

	return &SyncStatus{
		OrgID:    orgID,
		SyncedAt: &syncedAt,
		TagKeys:  tagKeys,
	}, nil
}

func (s *SyncService) loadPreviousTagKeys(ctx context.Context, orgID string) ([]TagKeyCatalog, error) {
	var tagKeysRaw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT tag_keys FROM org_tag_sync_metadata WHERE org_id = $1`, orgID,
	).Scan(&tagKeysRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeTagKeys(tagKeysRaw)
}

func logRemovedTagKeys(orgID string, previous, current []TagKeyCatalog) {
	if len(previous) == 0 {
		return
	}
	currentKeys := make(map[string]struct{}, len(current))
	for _, entry := range current {
		currentKeys[entry.Key] = struct{}{}
	}
	for _, entry := range previous {
		if _, ok := currentKeys[entry.Key]; !ok {
			log.Infof("tag sync: removed tag key %q for org %q", entry.Key, orgID)
		}
	}
}

func normalizeTagKeys(tagKeys []TagKeyCatalog) []TagKeyCatalog {
	if len(tagKeys) == 0 {
		return []TagKeyCatalog{}
	}
	out := make([]TagKeyCatalog, 0, len(tagKeys))
	for _, entry := range tagKeys {
		key := strings.TrimSpace(entry.Key)
		if key == "" {
			continue
		}
		values := entry.Values
		if values == nil {
			values = []string{}
		}
		out = append(out, TagKeyCatalog{Key: key, Values: values})
	}
	return out
}

func normalizeNamespaceTags(tags map[string]string) map[string]string {
	if tags == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func parseSyncedAt(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid synced_at timestamp %q: %w", raw, err)
	}
	return parsed.UTC(), nil
}

func decodeTagKeys(raw []byte) ([]TagKeyCatalog, error) {
	if len(raw) == 0 {
		return []TagKeyCatalog{}, nil
	}
	var tagKeys []TagKeyCatalog
	if err := json.Unmarshal(raw, &tagKeys); err != nil {
		return nil, err
	}
	return normalizeTagKeys(tagKeys), nil
}
