package tags

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// APITagProvider resolves tags from org_container_keys.resolved_tags populated by Koku push sync.
type APITagProvider struct {
	pool *pgxpool.Pool
}

// NewAPITagProvider constructs a push-sync tag provider.
func NewAPITagProvider(pool *pgxpool.Pool) *APITagProvider {
	return &APITagProvider{pool: pool}
}

func (p *APITagProvider) GetEnabledTagKeys(ctx context.Context, orgID string) ([]string, error) {
	catalog, err := p.TagCatalog(ctx, orgID)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(catalog))
	for _, entry := range catalog {
		keys = append(keys, entry.Key)
	}
	return keys, nil
}

func (p *APITagProvider) GetTagValues(ctx context.Context, orgID string, key string) ([]string, error) {
	catalog, err := p.TagCatalog(ctx, orgID)
	if err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	for _, entry := range catalog {
		if entry.Key == key {
			return entry.Values, nil
		}
	}
	return []string{}, nil
}

func (p *APITagProvider) FilterByTag(ctx context.Context, orgID string, key string, values []string) ([]string, error) {
	if p == nil || p.pool == nil {
		return nil, fmt.Errorf("api tag provider is not configured")
	}
	orgID = trimOrgID(orgID)
	key = strings.TrimSpace(key)
	if orgID == "" {
		return nil, fmt.Errorf("org_id is required")
	}
	if key == "" {
		return nil, fmt.Errorf("tag key is required")
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("tag filter %q requires at least one value", key)
	}

	var query string
	var args []interface{}
	if len(values) == 1 && values[0] == "*" {
		query = `
			SELECT DISTINCT namespace || '/' || workload || '/' || container_name
			FROM org_container_keys
			WHERE org_id = $1 AND resolved_tags ? $2`
		args = []interface{}{orgID, key}
	} else if len(values) == 1 {
		payload, err := json.Marshal(map[string]string{key: values[0]})
		if err != nil {
			return nil, fmt.Errorf("marshal tag filter: %w", err)
		}
		query = `
			SELECT DISTINCT namespace || '/' || workload || '/' || container_name
			FROM org_container_keys
			WHERE org_id = $1 AND resolved_tags @> $2::jsonb`
		args = []interface{}{orgID, string(payload)}
	} else {
		query = `
			SELECT DISTINCT namespace || '/' || workload || '/' || container_name
			FROM org_container_keys
			WHERE org_id = $1 AND resolved_tags->>$2 = ANY($3)`
		args = []interface{}{orgID, key, values}
	}

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("filter containers by resolved_tags for org %q: %w", orgID, err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var containerKey string
		if err := rows.Scan(&containerKey); err != nil {
			return nil, fmt.Errorf("scan container key: %w", err)
		}
		keys = append(keys, containerKey)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate container keys: %w", err)
	}
	return keys, nil
}

func (p *APITagProvider) TagCatalog(ctx context.Context, orgID string) ([]TagKeyCatalog, error) {
	if p == nil || p.pool == nil {
		return nil, fmt.Errorf("api tag provider is not configured")
	}
	orgID = trimOrgID(orgID)
	if orgID == "" {
		return nil, fmt.Errorf("org_id is required")
	}

	var tagKeysRaw []byte
	err := p.pool.QueryRow(ctx, `
		SELECT tag_keys FROM org_tag_sync_metadata WHERE org_id = $1`, orgID,
	).Scan(&tagKeysRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return []TagKeyCatalog{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load tag catalog for org %q: %w", orgID, err)
	}
	return decodeTagKeys(tagKeysRaw)
}
