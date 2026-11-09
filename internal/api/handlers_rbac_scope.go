package api

import (
	"context"

	"github.com/labstack/echo/v4"
)

// dataClusterScope resolves the caller's allowed clusters for any data
// reader that consumes them (#752; promoted from #753's
// snapshotDataClusterScope). It is the shared implementation of ADR-0352's
// helper contract: server-owned resource scope for list/count/group/CSV and
// resolved-ID detail paths — a client-supplied cluster filter is never a
// substitute for this scope (they compose: the client filter intersects the
// allowed set).
//
// scoped is false when no filtering applies (RBAC disabled or a wildcard);
// scoped with an empty list means DENY — settings-only identities and
// explicitly-empty grants get no data (eligibility is inherited transitively
// through filterClustersByRBAC).
//
// Empty-set handling differs by caller shape: the SQL readers in this
// package append `= ANY($N)` and let an empty array match nothing (correct
// empty shapes for free); builders that OMIT the clause for empty slices
// (the engine's `len(x) > 0` pattern, e.g. GPU MIG) must early-return their
// empty shape instead — see ADR-0352's consequences.
func dataClusterScope(ctx context.Context, orgID string, userPerms map[string][]string) ([]string, bool, error) {
	if !fleetSummaryNeedsClusterFilter(userPerms) {
		return nil, false, nil
	}
	all, err := getClustersForOrg(ctx, orgID)
	if err != nil {
		return nil, true, err
	}
	return filterClustersByRBAC(all, userPerms), true, nil
}

// readerClusterAllowed reports whether the caller's server-owned cluster
// scope admits the given cluster (#752). Detail and trend paths use this to
// return their not-found response for out-of-scope resolves —
// indistinguishable from unknown IDs (Koku's queryset convention: an
// RBAC-filtered queryset makes get_object 404).
func readerClusterAllowed(c echo.Context, cluster string) bool {
	userPerms := get_user_permissions(c)
	if !fleetSummaryNeedsClusterFilter(userPerms) {
		return true
	}
	return len(filterClustersByRBAC([]string{cluster}, userPerms)) > 0
}
