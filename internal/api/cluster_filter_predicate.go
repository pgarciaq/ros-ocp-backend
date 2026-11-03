package api

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// clusterFilterPredicate builds the client cluster-filter predicate for the
// raw-SQL readers (#790: nodes, pvcs, snapshots, quota, cluster-quota).
//
// Resolution matches the seven working endpoints (containers, namespaces,
// quality, history) — parseClusterParams' shape (utils.go:337) with
// FilterModeInclude semantics (common.go:73: ILIKE, wrapped):
//
//   - a UUID value binds exactly against the column (the already-working
//     filter form, unchanged);
//   - a non-UUID value (the cluster ALIAS the UI sends) resolves through
//     the clusters table subquery — never against the uuid column (binding
//     an alias there fails at Postgres cast time, SQLSTATE 22P02 → 503,
//     the bug this fixes). The subquery is ORG-SCOPED (#790 review: the
//     schema permits the same cluster_uuid under two tenants — #552's
//     colliding-UUID fixtures — so an unscoped lookup would let one tenant
//     infer another tenant's aliases; the working endpoints'
//     parseClusterParams shares that weakness and is tracked separately)
//     and uses ILIKE with %wrapping%, matching the include-mode filter the
//     container endpoints apply (case-insensitive substring — Koku's
//     contract: provider_map.py icontains on cluster_alias/cluster_id).
//
// Contract for unknown aliases: the subquery matches nothing → the caller
// returns 200 + empty. No 4xx.
//
// The predicate AND-composes with the #752 server-scope clauses: the client
// filter resolves which cluster the user ASKED for; the server scope bounds
// what they may SEE. An A-only grant filtering for B's alias gets empty.
//
// ok is false for an empty value — the caller skips the predicate entirely.
func clusterFilterPredicate(orgID, column, value string, argIdx int) (sql string, args []any, nextIdx int, ok bool) {
	if value == "" {
		return "", nil, argIdx, false
	}
	if _, err := uuid.Parse(value); err == nil {
		return " AND " + column + " = $" + strconv.Itoa(argIdx), []any{value}, argIdx + 1, true
	}
	return " AND " + column + " IN (SELECT cluster_uuid FROM clusters WHERE org_id = $" + strconv.Itoa(argIdx) +
			" AND cluster_alias ILIKE $" + strconv.Itoa(argIdx+1) + ")",
		[]any{orgID, "%" + value + "%"}, argIdx + 2, true
}

// resolveClusterFilterUUID resolves a client cluster filter to the UUID the
// currency layer keys on (#790 review: fetchClusterCurrency receives the
// raw filter value; an alias fails the Koku cluster lookup and silently
// falls back to USD, mislabeling non-USD savings). A UUID filter passes
// through unchanged; an alias resolves to its org-scoped ILIKE match (the
// same semantics as clusterFilterPredicate); no match or an empty filter
// resolves to "" (org-level default — the same behavior as an absent
// filter, since a filter that matches nothing has no rows to price).
func resolveClusterFilterUUID(ctx context.Context, pool *pgxpool.Pool, orgID, value string) string {
	if value == "" {
		return ""
	}
	if _, err := uuid.Parse(value); err == nil {
		return value
	}
	var resolved string
	// A substring alias may match several clusters; the first match's
	// currency is used (single-cluster semantics, matching the UUID form's
	// behavior for one cluster; multi-match filters span clusters whose
	// currencies may differ — org-level default would lose information for
	// the common single-match case).
	err := pool.QueryRow(ctx, `
		SELECT cluster_uuid::text FROM clusters
		WHERE org_id = $1 AND cluster_alias ILIKE $2
		LIMIT 1`, orgID, "%"+value+"%").Scan(&resolved)
	if err != nil {
		return ""
	}
	return resolved
}
