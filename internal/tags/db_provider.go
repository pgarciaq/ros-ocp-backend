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
