package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
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

// resolveClusterAliasScope resolves filter[cluster] to the clusters a request
// may read: every org-scoped cluster matching the value (a UUID binds exactly,
// anything else is a case-insensitive substring over cluster_alias — the
// #790/#947 union contract), intersected with allowed.
//
// unrestricted says the caller holds no granted set at all (an org admin), in
// which case the resolved matches are the scope. A restricted caller with an
// EMPTY allowed set gets an empty result: an absent grant is not unrestricted
// access (#753), so emptiness is never inferred as "all clusters".
//
// A lookup failure is returned, never flattened into "no match" (#953): the
// result scopes rows, so "I could not look" must not render as an empty page.
// No match returns an empty slice with a nil error — the caller renders its
// surface's empty outcome (#790).
func resolveClusterAliasScope(ctx context.Context, pool *pgxpool.Pool, orgID string, allowed []string, unrestricted bool, filter string) ([]string, error) {
	if filter == "" {
		return allowed, nil
	}
	match, err := resolveClusterFilterUUIDs(ctx, pool, orgID, filter)
	if err != nil {
		return nil, err
	}
	if unrestricted {
		return match, nil
	}
	return intersectClusterUUIDs(allowed, match), nil
}

// clusterScopeForFilter resolves a client cluster value to the clusters a
// request may read, fetching the org's cluster list only when RBAC actually
// narrows it. An unrestricted caller filtering by UUID therefore needs no
// lookup at all — the value binds exactly and nothing intersects it, which
// keeps these readers working on a cluster whose `clusters` row is not
// registered yet (a UUID used to be a direct bind everywhere).
//
// A lookup failure is returned, never flattened into "no match" (#953).
func clusterScopeForFilter(ctx context.Context, pool *pgxpool.Pool, orgID string, userPerms map[string][]string, filter string) ([]string, error) {
	unrestricted := !fleetSummaryNeedsClusterFilter(userPerms)
	var allowed []string
	if !unrestricted {
		all, err := getClustersForOrg(ctx, orgID)
		if err != nil {
			return nil, err
		}
		allowed = filterClustersByRBAC(all, userPerms)
	}
	return resolveClusterAliasScope(ctx, pool, orgID, allowed, unrestricted, filter)
}

// detailCluster is the resolved scope of a client-supplied cluster value on a
// single-object detail route.
type detailCluster struct {
	UUID      string
	Found     bool
	Ambiguous int // number of readable matches when > 1
}

// resolveDetailCluster maps a client cluster value on a single-object detail
// route to exactly one readable cluster (#967). Those routes render one
// object, so a filter naming several clusters has no truthful answer:
// picking one would return another cluster's object of the same name (or
// merge two clusters' rows), which is the silent wrong answer the alias
// contract exists to remove.
//
// A lookup failure is an error, not "not found" (#953).
func resolveDetailCluster(ctx context.Context, pool *pgxpool.Pool, orgID string, userPerms map[string][]string, filter string) (detailCluster, error) {
	scoped, err := clusterScopeForFilter(ctx, pool, orgID, userPerms, filter)
	if err != nil {
		return detailCluster{}, err
	}
	switch len(scoped) {
	case 0:
		return detailCluster{}, nil
	case 1:
		return detailCluster{UUID: scoped[0], Found: true}, nil
	default:
		return detailCluster{Ambiguous: len(scoped)}, nil
	}
}

// clusterFilterAmbiguousMessage is the shared 400 body for surfaces that can
// render only one cluster — a single-object detail, or a per-cluster series
// whose rows carry no cluster column. On those, an alias resolving to several
// clusters has no truthful answer: picking one merges or hides another
// cluster's data, which is the failure mode the alias contract exists to
// remove. The caller is told the count and the way out.
//
// Union surfaces (lists whose rows carry the cluster, and aggregates) never
// reach this: they return every match.
func clusterFilterAmbiguousMessage(filter string, matches int) string {
	return fmt.Sprintf(
		"filter[cluster] %q resolves to %d clusters; this surface returns one cluster — pass a cluster UUID or a narrower alias",
		filter, matches,
	)
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
		// No match is not a failure — an unknown alias legitimately has no
		// rows to price. Neither is a client that hung up: the query is
		// canceled, the response goes nowhere, and clients disconnect often
		// enough that warning on it would bury the real degradations.
		// Anything else is: it degrades the label to the default, and an
		// unlogged degradation is indistinguishable from "no filter" the next
		// time someone debugs a wrong currency (#953).
		if !errors.Is(err, pgx.ErrNoRows) &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			logging.ForOrgOnly(orgID).Warnf(
				"cluster filter %q did not resolve for the display currency: %v (falling back to the default currency label)",
				value, err)
		}
		return ""
	}
	return resolved
}
