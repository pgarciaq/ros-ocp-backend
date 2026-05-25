package tags

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	kokuEnabledTagKeysTable = "reporting_enabledtagkeys"
	kokuTagValuesTable      = "reporting_ocptags_values"
)

// DBTagProvider reads tag keys and values directly from Koku tenant tables in the shared PostgreSQL.
type DBTagProvider struct {
	pool *pgxpool.Pool
}

// NewDBTagProvider constructs a direct-database tag provider.
func NewDBTagProvider(pool *pgxpool.Pool) *DBTagProvider {
	return &DBTagProvider{pool: pool}
}

func (p *DBTagProvider) GetEnabledTagKeys(ctx context.Context, orgID string) ([]string, error) {
	if p == nil || p.pool == nil {
		return nil, fmt.Errorf("db tag provider is not configured")
	}
	schema, err := TenantSchema(orgID)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT key
		FROM %s
		WHERE enabled = true AND provider_type = 'OCP'
		ORDER BY key`, pgx.Identifier{schema, kokuEnabledTagKeysTable}.Sanitize())

	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query enabled tag keys for org %q: %w", orgID, err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan enabled tag key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enabled tag keys: %w", err)
	}
	return keys, nil
}

func (p *DBTagProvider) GetTagValues(ctx context.Context, orgID string, key string) ([]string, error) {
	if p == nil || p.pool == nil {
		return nil, fmt.Errorf("db tag provider is not configured")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("tag key is required")
	}
	schema, err := TenantSchema(orgID)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT value
		FROM %s
		WHERE key = $1
		ORDER BY value`, pgx.Identifier{schema, kokuTagValuesTable}.Sanitize())

	rows, err := p.pool.Query(ctx, query, key)
	if err != nil {
		return nil, fmt.Errorf("query tag values for org %q key %q: %w", orgID, key, err)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan tag value: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tag values: %w", err)
	}
	return values, nil
}

func (p *DBTagProvider) FilterByTag(ctx context.Context, orgID string, key string, values []string) ([]string, error) {
	if p == nil || p.pool == nil {
		return nil, fmt.Errorf("db tag provider is not configured")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("tag key is required")
	}
	orgID = trimOrgID(orgID)
	if orgID == "" {
		return nil, fmt.Errorf("org_id is required")
	}

	query, args, err := p.containerKeysForTagQuery(orgID, key, values)
	if err != nil {
		return nil, err
	}

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("filter containers by tag for org %q: %w", orgID, err)
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

func (p *DBTagProvider) TagCatalog(ctx context.Context, orgID string) ([]TagKeyCatalog, error) {
	enabledKeys, err := p.GetEnabledTagKeys(ctx, orgID)
	if err != nil {
		return nil, err
	}
	catalog := make([]TagKeyCatalog, 0, len(enabledKeys))
	for _, key := range enabledKeys {
		values, err := p.GetTagValues(ctx, orgID, key)
		if err != nil {
			return nil, err
		}
		catalog = append(catalog, TagKeyCatalog{Key: key, Values: values})
	}
	return catalog, nil
}

func (p *DBTagProvider) containerKeysForTagQuery(orgID, key string, values []string) (string, []interface{}, error) {
	schema, err := TenantSchema(orgID)
	if err != nil {
		return "", nil, err
	}
	tagValuesTable := pgx.Identifier{schema, kokuTagValuesTable}.Sanitize()

	var matchClause string
	args := []interface{}{orgID, key}
	if len(values) == 1 && values[0] == "*" {
		matchClause = "tv.key = $2"
	} else if len(values) > 0 {
		matchClause = "tv.key = $2 AND tv.value = ANY($3)"
		args = append(args, values)
	} else {
		return "", nil, fmt.Errorf("tag filter %q requires at least one value", key)
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT ock.namespace || '/' || ock.workload || '/' || ock.container_name
		FROM org_container_keys ock
		WHERE ock.org_id = $1
		  AND EXISTS (
			SELECT 1
			FROM %s tv,
			     unnest(tv.cluster_ids, tv.namespaces) AS t(cluster_id, namespace)
			WHERE %s
			  AND t.cluster_id = ock.cluster_uuid::text
			  AND t.namespace = ock.namespace
		  )`, tagValuesTable, matchClause)
	return query, args, nil
}
