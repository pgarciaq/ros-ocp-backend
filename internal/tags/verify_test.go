package tags

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestVerifyDBAccess_missingTable(t *testing.T) {
	if testing.Short() {
		t.Skip("requires testcontainers")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (99, '9999999') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	err = VerifyDBAccess(ctx, pool)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reporting_enabledtagkeys")
}

func TestVerifyDBAccess_tableExists(t *testing.T) {
	if testing.Short() {
		t.Skip("requires testcontainers")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (98, '1234567') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS org1234567`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS org1234567.reporting_enabledtagkeys (
			id serial PRIMARY KEY,
			key text NOT NULL,
			enabled boolean NOT NULL DEFAULT true,
			provider_type text NOT NULL DEFAULT 'OCP'
		)`)
	require.NoError(t, err)

	require.NoError(t, VerifyDBAccess(ctx, pool))
}
