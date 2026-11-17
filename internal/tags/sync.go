package tags

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

// TagValue unmarshals a tag value in either shape (#814 D4 tolerance): a
// JSON string (the pre-#814 koku payload, and the scalar legacy) or a JSON
// array of strings (the cycle-2 koku payload). A JSON null element decodes
// as the empty string (Go's null-into-string no-op) rather than erroring —
// benign only because normalization drops empty values; any other
// non-string element is rejected — the contract is string-valued tags.
type TagValue []string

func (t *TagValue) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*t = []string{single}
		return nil
	}
	var multi []string
	if err := json.Unmarshal(data, &multi); err == nil {
		*t = multi
		return nil
	}
	return fmt.Errorf("tag value must be a string or an array of strings, got %s", data)
}

// MarshalJSON writes the normalized array form (#814 D1: values are
// sorted-unique arrays in storage).
func (t TagValue) MarshalJSON() ([]byte, error) {
	return json.Marshal([]string(t))
}

// NamespaceTags holds resolved tag key/value pairs for all containers in a namespace.
type NamespaceTags struct {
	ClusterUUID string              `json:"cluster_uuid"`
	Namespace   string              `json:"namespace"`
	Tags        map[string]TagValue `json:"tags"`
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
	defer func() { _ = tx.Rollback(ctx) }()

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

// normalizeTagKeys applies the same D3 defense to the catalog half of the
// payload: sorted-unique values, empty values dropped (a cycle-2 koku
// sending unsorted or duplicate catalog values must not store them raw —
// the catalog feeds the tag-values API).
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
		seen := make(map[string]struct{}, len(entry.Values))
		values := make([]string, 0, len(entry.Values))
		for _, v := range entry.Values {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			values = append(values, v)
		}
		sort.Strings(values)
		out = append(out, TagKeyCatalog{Key: key, Values: values})
	}
	return out
}

// normalizeNamespaceTags normalizes to sorted-unique arrays (#814 D1/D3):
// whatever shape arrives — scalar values from the pre-#814 koku builder,
// unsorted or duplicate arrays from cycle 2 — the storage contract is
// always {"key": ["sorted", "unique", "values"]}. The sorted order also
// makes the first element the alphabetically-first value, which the
// enrichment projection (D5 phase 1) uses for the scalar response shape.
func normalizeNamespaceTags(tags map[string]TagValue) map[string]TagValue {
	if tags == nil {
		return map[string]TagValue{}
	}
	out := make(map[string]TagValue, len(tags))
	for key, values := range tags {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		seen := make(map[string]struct{}, len(values))
		unique := make([]string, 0, len(values))
		for _, v := range values {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			unique = append(unique, v)
		}
		if len(unique) == 0 {
			continue
		}
		sort.Strings(unique)
		out[key] = TagValue(unique)
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
