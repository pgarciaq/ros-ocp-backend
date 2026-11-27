package api

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
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

	// A legitimate no-match is not a failure and must stay out of the log: an
	// unknown alias is a documented 200 + empty, and warning on it would make
	// the degradation warning (#953) useless noise.
	var buf bytes.Buffer
	prev := logging.GetLogger().Logger.Out
	if prev == nil {
		prev = os.Stderr
	}
	logging.SetOutput(&buf)
	t.Cleanup(func() { logging.SetOutput(prev) })
	assert.Equal(t, "", resolveClusterFilterUUID(ctx, pool, orgID, "no-such-alias"),
		"no match resolves to the default label")
	assert.NotContains(t, buf.String(), "did not resolve",
		"no match (pgx.ErrNoRows) is not a lookup failure and must not warn (#953)")
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
		got, qErr := resolveClusterFilterUUIDs(ctx, pool, orgID, strings.ToUpper(clusterC))
		require.NoError(t, qErr)
		assert.Equal(t, []string{clusterC}, got,
			"an uppercase UUID filter must return the cluster, not no rows (#951)")
	})

	t.Run("alias substring returns every match", func(t *testing.T) {
		got, qErr := resolveClusterFilterUUIDs(ctx, pool, orgID, "a")
		require.NoError(t, qErr)
		assert.ElementsMatch(t, []string{testutil.TestClusterUUID, clusterB, clusterC}, got,
			"union semantics: every matching cluster, not the first (#947)")
	})

	t.Run("unknown alias resolves to nothing", func(t *testing.T) {
		got, qErr := resolveClusterFilterUUIDs(ctx, pool, orgID, "no-such-cluster")
		require.NoError(t, qErr)
		assert.Nil(t, got, "no match is not an error — the caller renders 200 + empty (#790)")
	})
}

// TestResolveClusterFilterUUIDsReportsLookupFailure: a failed lookup must be
// reported, not rendered as "nothing matched" (#953). A lazily-connected pool
// against a dead DSN fails at Query, so this needs no container.
func TestResolveClusterFilterUUIDsReportsLookupFailure(t *testing.T) {
	dead, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/db?connect_timeout=2")
	require.NoError(t, err)
	defer dead.Close()

	got, qErr := resolveClusterFilterUUIDs(context.Background(), dead, "org-1", "team-a")
	require.Error(t, qErr, "a failed lookup must not be reported as 'nothing matched' (#953)")
	assert.Nil(t, got)
	assert.Contains(t, qErr.Error(), "team-a",
		"the error names the filter it failed to resolve, so the log identifies the request (#953)")
}

// TestResolveClusterFilterUUIDFirstMatchIsDeterministic: a substring alias can
// match several clusters, and the currency label keys on the first one. With no
// ORDER BY that is heap order — the same request can be priced in different
// currencies across calls and replicas (#954a). The first match is pinned to
// the lowest canonical cluster UUID.
func TestResolveClusterFilterUUIDFirstMatchIsDeterministic(t *testing.T) {
	t.Setenv("RBAC_ENABLE", "false")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const orgID = "org-resolver-order"
	const small = "11111111-1111-4111-8111-111111111111"
	const large = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	// The LARGER uuid is inserted first and carries the alphabetically earlier
	// alias: both plausible plans (heap order, alias-ordered index) return it,
	// so the assertion fails pre-fix on this fixture instead of by luck.
	for _, cl := range []struct{ uuid, alias string }{
		{large, "cur-first"},
		{small, "cur-second"},
	} {
		_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
			VALUES (1, $2, $1, $3, 'src-r', now()) ON CONFLICT DO NOTHING`, cl.uuid, orgID, cl.alias)
		require.NoError(t, err)
	}

	assert.Equal(t, small, resolveClusterFilterUUID(ctx, pool, orgID, "cur-"),
		"first match is the lowest canonical UUID, not heap order (#954a)")
}

// TestResolveClusterFilterUUIDDegradesOnLookupFailure pins the deliberate
// asymmetry with resolveClusterFilterUUIDs (#953): the display-currency label
// is best-effort — it never fails the request — but the degradation must be
// VISIBLE, because an unlogged "" is indistinguishable from "no filter" when
// someone debugs a wrong currency.
func TestResolveClusterFilterUUIDDegradesOnLookupFailure(t *testing.T) {
	var buf bytes.Buffer
	// Capture the process logger, then restore whatever writer was there:
	// this package has parallel tests, and the default is not ours to assume.
	prev := logging.GetLogger().Logger.Out
	if prev == nil {
		prev = os.Stderr
	}
	logging.SetOutput(&buf)
	t.Cleanup(func() { logging.SetOutput(prev) })

	dead, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/db?connect_timeout=2")
	require.NoError(t, err)
	defer dead.Close()

	assert.Equal(t, "", resolveClusterFilterUUID(context.Background(), dead, "org-1", "team-a"),
		"the currency label degrades to the org default; it does not fail the request (#953)")
	out := buf.String()
	assert.Contains(t, out, "team-a",
		"the warning names the filter value that could not be resolved")
	assert.Contains(t, out, "currency",
		"the warning says what degraded, so the log does not read as a cluster-scope failure (#953)")
}
