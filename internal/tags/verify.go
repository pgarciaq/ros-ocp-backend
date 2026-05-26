package tags

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// VerifyDBAccess checks that Koku tag tables are reachable in a tenant schema.
// It loads the first org_id from rh_accounts and probes reporting_enabledtagkeys.
func VerifyDBAccess(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("database pool is not configured")
	}

	var orgID string
	err := pool.QueryRow(ctx, `SELECT org_id FROM rh_accounts ORDER BY id LIMIT 1`).Scan(&orgID)
	if err != nil {
		return fmt.Errorf("load org from rh_accounts: %w", err)
	}

	schema, err := TenantSchema(orgID)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(
		`SELECT 1 FROM %s LIMIT 1`,
		pgx.Identifier{schema, kokuEnabledTagKeysTable}.Sanitize(),
	)

	var probe int
	err = pool.QueryRow(ctx, query).Scan(&probe)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Table exists but has no enabled keys — connectivity is OK.
			return nil
		}
		return fmt.Errorf("query %s.%s: %w", schema, kokuEnabledTagKeysTable, err)
	}
	return nil
}
