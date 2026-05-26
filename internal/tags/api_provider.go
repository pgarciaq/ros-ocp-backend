package tags

import (
	"context"
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
	catalog, err := p.tagCatalog(ctx, orgID)
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
	catalog, err := p.tagCatalog(ctx, orgID)
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

func (p *APITagProvider) tagCatalog(ctx context.Context, orgID string) ([]TagKeyCatalog, error) {
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
