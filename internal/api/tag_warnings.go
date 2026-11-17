package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

const (
	tagStatusEndpoint = "GET /api/cost-management/v1/internal/tags/status"
	tagSyncStaleAfter = 6 * time.Hour
)

// tagWarningScope is the caller's data-derived tag-key catalog scope
// (#828 cluster grain, #834 project grain). Per dimension:
//   - nil    = unscoped (all clusters / all projects)
//   - empty  = scoped to NOTHING (denied identities; an empty project
//     grant is no access — Koku semantics). The empty set must never read
//     as unscoped.
//
// Node grain is the documented remaining limitation (#834): a data-model
// constraint — org_container_keys has no node dimension — not a policy
// choice.
type tagWarningScope struct {
	clusters   []string
	namespaces []string
}

// tagWarningScopeResolver lazily resolves the caller's scope. Contract:
//   - (zero scope, nil)       = unscoped: the org-wide data-derived catalog
//     applies (RBAC disabled, wildcard, or unrestricted access).
//   - (non-nil dimensions, nil) = scoped to EXACTLY those clusters and/or
//     namespaces — including EMPTY slices, which scope to nothing. An
//     empty set must never read as unscoped.
//   - (_, err)                = scope unresolvable: buildTagWarnings
//     suppresses warnings entirely — no existence-revealing diagnostic
//     fires when the scope cannot be established (fail-silent in the safe
//     direction).
//
// The resolver runs lazily, only after buildTagWarnings' early-outs, so
// callers without a materialized cluster list pay no query on the happy path.
type tagWarningScopeResolver func(ctx context.Context) (tagWarningScope, error)

// projectNamespaceScope derives the namespace (project) dimension from the
// caller's perms, mirroring query_builder's applyProjectFilter semantics
// exactly (#834): absent grants, "*", or a "*" entry mean unrestricted
// (nil); an explicit list scopes; an EMPTY grant is no access (the empty
// list, not nil) — Koku treats an empty grant as no access.
//
// Eligibility is deliberately NOT checked here, and that skip is safe only
// through an invariant: an ineligible identity either has no project grant
// (nil here) or an empty one ([] — scoped to nothing), and in the first
// case the CLUSTER dimension transitively catches the deny, because every
// caller derives its cluster-scoped flag from fleetSummaryNeedsClusterFilter
// or dataClusterScope, both of which return scoped for ineligible
// identities (filterClustersByRBAC → empty). A future caller passing
// clusterScoped=false for an ineligible, project-absent identity would
// resurrect the #828 bug class through the requestTagWarningScope gate —
// do not add such a caller without re-checking this.
func projectNamespaceScope(userPerms map[string][]string) []string {
	cfg := config.GetConfig()
	if !cfg.RBACEnabled {
		return nil
	}
	if _, ok := userPerms["*"]; ok {
		return nil
	}
	projectPerms, ok := userPerms["openshift.project"]
	if !ok || utils.StringInSlice("*", projectPerms) {
		return nil
	}
	return projectPerms
}

// staticTagWarningScope wraps an already-resolved scope (callers that hold
// the RBAC-filtered cluster list from their own data query). clusterScoped
// gates only the cluster half — the namespace dimension applies
// regardless, so a cluster-unrestricted but project-restricted caller is
// still narrowed (#834). scoped=false means cluster-unscoped; a scoped nil
// cluster list is materialized as empty — the denied case #828 fixed
// (filterClustersByRBAC returns nil for denied identities, which the
// catalog query must not read as org-wide).
func staticTagWarningScope(userPerms map[string][]string, clusterScoped bool, allowedClusters []string) tagWarningScopeResolver {
	namespaces := projectNamespaceScope(userPerms)
	return func(context.Context) (tagWarningScope, error) {
		var scope tagWarningScope
		if clusterScoped {
			if allowedClusters == nil {
				allowedClusters = []string{}
			}
			scope.clusters = allowedClusters
		}
		scope.namespaces = namespaces
		return scope, nil
	}
}

// requestTagWarningScope resolves the caller's scope from the request on
// demand (#828, #834): unscoped identities short-circuit without a query;
// scoped identities resolve the org's clusters and RBAC-filter them once,
// lazily. The cluster query runs only when the cluster half is scoped — a
// cluster-unrestricted but project-restricted caller gets the namespace
// filter alone, with no cluster query.
func requestTagWarningScope(c echo.Context, orgID string) tagWarningScopeResolver {
	userPerms := get_user_permissions(c)
	clusterScoped := fleetSummaryNeedsClusterFilter(userPerms)
	namespaces := projectNamespaceScope(userPerms)
	if !clusterScoped && namespaces == nil {
		return func(context.Context) (tagWarningScope, error) { return tagWarningScope{}, nil }
	}
	return func(ctx context.Context) (tagWarningScope, error) {
		var scope tagWarningScope
		scope.namespaces = namespaces
		if clusterScoped {
			all, err := getClustersForOrg(ctx, orgID)
			if err != nil {
				return scope, err
			}
			allowed := filterClustersByRBAC(all, userPerms)
			if allowed == nil {
				allowed = []string{}
			}
			scope.clusters = allowed
		}
		return scope, nil
	}
}

// buildTagWarnings returns meta.warnings when tag filtering is active, results are empty,
// and the catalog or sync state suggests the filter may be ineffective.
func buildTagWarnings(ctx context.Context, orgID string, requestedTagKeys []string, resultCount int, scopeResolver tagWarningScopeResolver) []string {
	if resultCount > 0 || len(requestedTagKeys) == 0 || !config.TagsFeatureEnabled() {
		return nil
	}

	pool := db.GetPool()
	if pool == nil {
		return nil
	}

	// #828/#834: resolve the scope only now (past the early-outs) so unscoped
	// happy paths never pay the cluster query; an unresolvable scope
	// suppresses warnings rather than falling back to org-wide.
	var scope tagWarningScope
	if scopeResolver != nil {
		var scopeErr error
		scope, scopeErr = scopeResolver(ctx)
		if scopeErr != nil {
			return nil
		}
	}

	known, err := knownTagKeysForOrg(ctx, pool, orgID, scope)
	if err != nil {
		return nil
	}

	var warnings []string
	for _, key := range requestedTagKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := known[key]; !ok {
			warnings = append(warnings, fmt.Sprintf(
				"Tag filtering is active but no data matches. The requested tag key %q is not in the known tag catalog. Verify tag sync status via %s",
				key, tagStatusEndpoint,
			))
		}
	}

	if config.TagsUsePushSync() {
		if syncWarn := tagSyncWarning(ctx, pool, orgID); syncWarn != "" {
			warnings = append(warnings, syncWarn)
		}
	}

	if len(warnings) == 0 {
		return nil
	}
	return warnings
}

func tagSyncWarning(ctx context.Context, pool *pgxpool.Pool, orgID string) string {
	svc := tags.NewSyncService(pool)
	status, err := svc.GetSyncStatus(ctx, orgID)
	if err != nil || status == nil || status.SyncedAt == nil {
		return fmt.Sprintf(
			"Tag filtering is active but no data matches. Tag push sync has not completed for this organization. Verify tag sync status via %s",
			tagStatusEndpoint,
		)
	}
	if time.Since(*status.SyncedAt) > tagSyncStaleAfter {
		return fmt.Sprintf(
			"Tag filtering is active but no data matches. Tag push sync is stale (last synced %s). Verify tag sync status via %s",
			status.SyncedAt.UTC().Format(time.RFC3339), tagStatusEndpoint,
		)
	}
	return ""
}

// knownTagKeysForOrg returns the caller's known tag-key set: the org-wide
// enabled-key catalog (Koku parity — org configuration, deliberately
// org-visible) unioned with the data-derived keys, scoped to the caller's
// cluster AND project sets (#828, #834). A nil dimension is unfiltered; an
// empty dimension matches nothing.
func knownTagKeysForOrg(ctx context.Context, pool *pgxpool.Pool, orgID string, scope tagWarningScope) (map[string]struct{}, error) {
	known := make(map[string]struct{})

	provider := tags.GetProvider()
	if provider != nil {
		catalog, err := tags.BuildTagCatalog(ctx, provider, orgID)
		if err == nil {
			for _, entry := range catalog {
				if entry.Key != "" {
					known[entry.Key] = struct{}{}
				}
			}
		}
	}

	// #752: cluster-scope the data-derived key set — a scoped empty
	// response must not reveal tag keys that exist elsewhere in the org.
	// #834: project-scope it the same way. NOTE the deliberate asymmetry
	// with list RESULTS on three routes: the PVC, node-utilization, and
	// GPU MIG data paths filter by cluster (and node) but NOT by project
	// (a pre-existing gap, filed separately), so on those routes the
	// catalog is now FINER than the results it annotates — over-warning
	// only, never a leak (a finer catalog reveals strictly less). The
	// container/namespace/history routes scope both halves.
	//
	// Placeholder numbering: the cluster clause is appended first, so its
	// placeholder is always $2; the namespace clause computes the next
	// free position from the args built so far. Keep the append order if
	// a third dimension is ever added.
	keyQuery := `
		SELECT DISTINCT jsonb_object_keys(resolved_tags)
		FROM org_container_keys
		WHERE org_id = $1`
	keyArgs := []any{orgID}
	if scope.clusters != nil {
		keyQuery += fmt.Sprintf(` AND cluster_uuid::text = ANY($%d)`, len(keyArgs)+1)
		keyArgs = append(keyArgs, scope.clusters)
	}
	if scope.namespaces != nil {
		keyQuery += fmt.Sprintf(` AND namespace = ANY($%d)`, len(keyArgs)+1)
		keyArgs = append(keyArgs, scope.namespaces)
	}

	rows, err := pool.Query(ctx, keyQuery, keyArgs...)
	if err != nil {
		return known, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return known, err
		}
		if key != "" {
			known[key] = struct{}{}
		}
	}
	return known, rows.Err()
}

func requestedTagKeysFromContext(c echo.Context) []string {
	filters, err := parseTagFiltersFromRequest(c)
	if err != nil || len(filters) == 0 {
		return nil
	}
	keys := make([]string, 0, len(filters))
	seen := make(map[string]struct{}, len(filters))
	for _, f := range filters {
		if f.Key == "" {
			continue
		}
		if _, ok := seen[f.Key]; ok {
			continue
		}
		seen[f.Key] = struct{}{}
		keys = append(keys, f.Key)
	}
	return keys
}

func attachTagWarningsToCollection[T any](resp *Collection[T], c echo.Context, orgID string, resultCount int, scope tagWarningScopeResolver) {
	if resp == nil {
		return
	}
	resp.Meta.Warnings = buildTagWarnings(c.Request().Context(), orgID, requestedTagKeysFromContext(c), resultCount, scope)
}

func attachTagWarningsToPVC(resp *PVCRecommendationListResponse, c echo.Context, orgID string, resultCount int, scope tagWarningScopeResolver) {
	if resp == nil {
		return
	}
	resp.Meta.Warnings = buildTagWarnings(c.Request().Context(), orgID, requestedTagKeysFromContext(c), resultCount, scope)
}

func attachTagWarningsToNodeUtil(resp *model.NodeUtilizationListResponse, c echo.Context, orgID string, resultCount int, scope tagWarningScopeResolver) {
	if resp == nil {
		return
	}
	tagWarnings := buildTagWarnings(c.Request().Context(), orgID, requestedTagKeysFromContext(c), resultCount, scope)
	if len(tagWarnings) == 0 {
		return
	}
	resp.Meta.Warnings = append(resp.Meta.Warnings, tagWarnings...)
}

func attachTagWarningsToGPUMIG(resp *model.GPUMIGListResponse, c echo.Context, orgID string, resultCount int, scope tagWarningScopeResolver) {
	if resp == nil {
		return
	}
	tagWarnings := buildTagWarnings(c.Request().Context(), orgID, requestedTagKeysFromContext(c), resultCount, scope)
	if len(tagWarnings) == 0 {
		return
	}
	resp.Meta.Warnings = append(resp.Meta.Warnings, tagWarnings...)
}
