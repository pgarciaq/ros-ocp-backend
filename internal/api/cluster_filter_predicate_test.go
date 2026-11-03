package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClusterFilterPredicate pins #790's shared client cluster-filter
// resolution. Each case's assertion names the pre-fix behavior it rules out:
// binding a non-UUID alias against a uuid column (SQLSTATE 22P02 → 503).
func TestClusterFilterPredicate(t *testing.T) {
	const testUUID = "4c5d2494-f20d-4b4a-9ed7-848954a2b103"
	const orgID = "org-predicate-unit"

	t.Run("uuid value binds exactly", func(t *testing.T) {
		sql, args, next, ok := clusterFilterPredicate(orgID, "cluster_uuid", testUUID, 5)
		require.True(t, ok)
		assert.Equal(t, " AND cluster_uuid = $5", sql,
			"a UUID filter keeps the exact bind — the already-working form is unchanged")
		assert.Equal(t, []any{testUUID}, args)
		assert.Equal(t, 6, next)
	})

	t.Run("alias value resolves through the org-scoped ILIKE subquery", func(t *testing.T) {
		sql, args, next, ok := clusterFilterPredicate(orgID, "nr.cluster_uuid", "sno", 2)
		require.True(t, ok)
		assert.Equal(t,
			" AND nr.cluster_uuid IN (SELECT cluster_uuid FROM clusters WHERE org_id = $2 AND cluster_alias ILIKE $3)",
			sql,
			"an alias resolves org-scoped with ILIKE %wrapping% — parseClusterParams' include-mode semantics (never bound against the uuid column; pre-fix: 22P02 → 503)")
		assert.Equal(t, []any{orgID, "%sno%"}, args)
		assert.Equal(t, 4, next)
	})

	t.Run("subquery is org-scoped", func(t *testing.T) {
		// #790 review Important 1: the schema permits the same cluster_uuid
		// under two tenants (#552 colliding-UUID fixtures) — an unscoped
		// lookup would let one tenant infer another tenant's aliases.
		sql, _, _, ok := clusterFilterPredicate(orgID, "cluster_uuid", "sno", 2)
		require.True(t, ok)
		assert.Contains(t, sql, "org_id = $2",
			"the subquery is org-scoped — cross-tenant alias inference closed")
	})

	t.Run("empty value signals skip", func(t *testing.T) {
		_, _, _, ok := clusterFilterPredicate(orgID, "cluster_uuid", "", 2)
		assert.False(t, ok, "an empty filter produces no predicate (caller skips)")
	})

	t.Run("uuid-looking values are not mangled", func(t *testing.T) {
		sql, args, _, ok := clusterFilterPredicate(orgID, "cluster_uuid", "4C5D2494-F20D-4B4A-9ED7-848954A2B103", 3)
		require.True(t, ok)
		assert.Contains(t, sql, "= $3")
		assert.Equal(t, []any{"4C5D2494-F20D-4B4A-9ED7-848954A2B103"}, args,
			"upper-case UUIDs bind exactly (pgx casts case-insensitively)")
	})
}
