package api

import (
	"context"
	"fmt"
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
		[]any{orgID, containsPattern(value)}, argIdx + 2, true
}

// canonicalClusterUUID returns the canonical lowercase dashed form of a UUID
// input and whether it parses as one (#951). Every comparison that compares
// cluster UUIDs as strings must go through it: Postgres compares uuid values
// canonically, Go compares strings byte-wise, so an uppercase or undashed
// input would otherwise match nothing against the lowercase cluster_uuid::text
// the cluster set is built from. The SQL predicate does not need it — it binds
// against a uuid column, which Postgres parses case-insensitively.
func canonicalClusterUUID(value string) (string, bool) {
	u, err := uuid.Parse(value)
	if err != nil {
		return "", false
	}
	return u.String(), true
}

// resolveClusterFilterUUIDs resolves a client cluster filter to EVERY
// org-scoped matching cluster UUID (#947: union semantics for substring
// aliases), canonicalized (#951). A UUID filter returns itself; an empty or
// no-match filter returns nil with a nil error — the #790 contract, where "no
// match" is 200 + empty at the caller.
//
// A lookup FAILURE is returned as an error (#953): both callers scope rows
// with this set, and "I could not look" must never render as "nothing
// matched". resolveClusterFilterUUID, which only labels the display currency,
// deliberately keeps the best-effort contract instead — the two resolvers are
// different contracts, not one contract in two arities.
func resolveClusterFilterUUIDs(ctx context.Context, pool *pgxpool.Pool, orgID, value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	if canon, ok := canonicalClusterUUID(value); ok {
		return []string{canon}, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT cluster_uuid::text FROM clusters
		WHERE org_id = $1 AND cluster_alias ILIKE $2`, orgID, containsPattern(value))
	if err != nil {
		return nil, fmt.Errorf("resolve cluster filter %q: %w", value, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan cluster uuid for filter %q: %w", value, err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster matches for filter %q: %w", value, err)
	}
	return out, nil
}

// intersectClusterUUIDs returns the members of allowed that appear in match,
// preserving allowed's order. An empty match yields an empty result.
func intersectClusterUUIDs(allowed, match []string) []string {
	set := make(map[string]struct{}, len(match))
	for _, m := range match {
		set[m] = struct{}{}
	}
	out := make([]string, 0, len(allowed))
	for _, a := range allowed {
		if _, ok := set[a]; ok {
			out = append(out, a)
		}
	}
	return out
}

// resolveClusterFilterUUID resolves a client cluster filter to the UUID the
// currency layer keys on (#790 review: fetchClusterCurrency receives the
// raw filter value; an alias fails the Koku cluster lookup and silently
// falls back to USD, mislabeling non-USD savings). A UUID filter passes
// through unchanged; an alias resolves to its org-scoped ILIKE match (the
// same semantics as clusterFilterPredicate); no match or an empty filter
// resolves to "" (org-level default — the same behavior as an absent
// filter, since a filter that matches nothing has no rows to price).
//
// This resolver is best-effort on purpose: it only LABELS the display
// currency, so a lookup failure degrades to the org default with a warning
// rather than failing the request. Contrast resolveClusterFilterUUIDs, which
// scopes rows and therefore must report failure (#953).
func resolveClusterFilterUUID(ctx context.Context, pool *pgxpool.Pool, orgID, value string) string {
	if value == "" {
		return ""
	}
	if canon, ok := canonicalClusterUUID(value); ok {
		return canon
	}
	var resolved string
	// A substring alias may match several clusters; the first match's
	// currency is used (single-cluster semantics, matching the UUID form's
	// behavior for one cluster; multi-match filters span clusters whose
	// currencies may differ — org-level default would lose information for
	// the common single-match case). "First" is the lowest canonical cluster
	// UUID, not heap order, so the label is stable across calls and replicas
	// (#954a). Preferring an exact alias over a substring match is a separate
	// question, out of scope here.
	err := pool.QueryRow(ctx, `
		SELECT cluster_uuid::text FROM clusters
		WHERE org_id = $1 AND cluster_alias ILIKE $2
		ORDER BY cluster_uuid
		LIMIT 1`, orgID, containsPattern(value)).Scan(&resolved)
	if err != nil {
		return ""
	}
	return resolved
}
