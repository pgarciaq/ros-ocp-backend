package api

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestResolveClusterFilterUUID: the resolver maps a client cluster filter
// to the UUID the currency layer keys on (#790 review Important 2: the raw
// alias previously reached fetchClusterCurrency, failed the Koku cluster
// lookup, and silently fell back to USD for non-USD orgs).
func TestResolveClusterFilterUUID(t *testing.T) {
	t.Setenv("RBAC_ENABLE", "false")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})
	ctx := context.Background()

	const orgID = "org-resolver-unit"
	const clusterB = "99999999-9999-9999-9999-999999999999"
	// clusterC carries hex letters on purpose: an all-digit UUID survives
	// strings.ToUpper unchanged, which would green the #951 assertion with the
	// bug still present (adversarial fixtures, docs/agents/adversarial-fixtures.md).
	const clusterC = "abcdef01-2345-4678-89ab-cdef01234567"
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	for _, cl := range []struct{ uuid, alias string }{
		{testutil.TestClusterUUID, "alpha-a"},
		{clusterB, "beta-b"},
		{clusterC, "gamma-g"},
	} {
		_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
			VALUES (1, $2, $1, $3, 'src-r', now()) ON CONFLICT DO NOTHING`, cl.uuid, orgID, cl.alias)
		require.NoError(t, err)
	}

	assert.Equal(t, clusterB, resolveClusterFilterUUID(ctx, pool, orgID, "beta-b"),
		"an alias resolves to its org-scoped cluster UUID (currency keys on the UUID, not the alias)")
	assert.Equal(t, clusterB, resolveClusterFilterUUID(ctx, pool, orgID, "BETA"),
		"partial/case-varied aliases resolve with the same ILIKE semantics as the predicate")
	assert.Equal(t, clusterB, resolveClusterFilterUUID(ctx, pool, orgID, clusterB),
		"a UUID filter passes through unchanged")
	assert.Equal(t, clusterC, resolveClusterFilterUUID(ctx, pool, orgID, strings.ToUpper(clusterC)),
		"a UUID filter is returned canonical — the downstream cluster lookup keys on the lowercase DB text (#951)")
	assert.Equal(t, "", resolveClusterFilterUUID(ctx, pool, orgID, "no-such-cluster"),
		"an unknown alias resolves to empty (org-level currency default)")
	assert.Equal(t, "", resolveClusterFilterUUID(ctx, pool, orgID, ""),
		"an empty filter resolves to empty")
	assert.Equal(t, "", resolveClusterFilterUUID(ctx, pool, "other-org", "beta-b"),
		"another org's alias does not resolve (org-scoped)")
}

// TestResolveClusterFilterUUIDs: the row-scope resolver returns EVERY match
// (#947), and a UUID filter comes back canonical (#951) — the set it is
// intersected with is lowercase cluster_uuid::text, so an uppercase filter
// would otherwise compare unequal in Go and silently return no rows.
func TestResolveClusterFilterUUIDs(t *testing.T) {
	t.Setenv("RBAC_ENABLE", "false")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})
	ctx := context.Background()

	const orgID = "org-resolver-uuids"
	const clusterB = "99999999-9999-9999-9999-999999999999"
	const clusterC = "abcdef01-2345-4678-89ab-cdef01234567" // hex letters: ToUpper must change it
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	for _, cl := range []struct{ uuid, alias string }{
		{testutil.TestClusterUUID, "alpha-a"},
		{clusterB, "beta-b"},
		{clusterC, "gamma-g"},
	} {
		_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
			VALUES (1, $2, $1, $3, 'src-r', now()) ON CONFLICT DO NOTHING`, cl.uuid, orgID, cl.alias)
		require.NoError(t, err)
	}

	t.Run("uppercase UUID filter returns the canonical uuid", func(t *testing.T) {
		got := resolveClusterFilterUUIDs(ctx, pool, orgID, strings.ToUpper(clusterC))
		assert.Equal(t, []string{clusterC}, got,
			"an uppercase UUID filter must return the cluster, not no rows (#951)")
	})

	t.Run("alias substring returns every match", func(t *testing.T) {
		got := resolveClusterFilterUUIDs(ctx, pool, orgID, "a")
		assert.ElementsMatch(t, []string{testutil.TestClusterUUID, clusterB, clusterC}, got,
			"union semantics: every matching cluster, not the first (#947)")
	})

	t.Run("unknown alias resolves to nothing", func(t *testing.T) {
		got := resolveClusterFilterUUIDs(ctx, pool, orgID, "no-such-cluster")
		assert.Nil(t, got, "no match is not an error — the caller renders 200 + empty (#790)")
	})
}
