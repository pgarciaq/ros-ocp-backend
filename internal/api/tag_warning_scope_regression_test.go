package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #828 (V16-01): the tag-warning catalog's data-derived key set must scope to
// the caller's server-owned cluster set on EVERY route that attaches tag
// warnings. A cluster-restricted caller must receive the SAME unknown-key
// treatment for a key observed only on an inaccessible cluster as for a
// nonexistent key — no existence oracle. The provider (enabled-key) catalog
// stays org-wide by design (Koku parity: Koku's own validation checks
// EnabledTagKeys org-wide); these tests seed no enabled keys, isolating the
// data-derived path.
const (
	v16BOnlyTagKey  = "b-secret-project"
	v16AOnlyTagKey  = "a-known-key"
	v16NoSuchTagKey = "no-such-tag"
)

// seedV16TagKeys seeds resolved_tags on both clusters: a B-only key (the
// oracle's secret) and an A-only key (the authorized-key control).
func seedV16TagKeys(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := database.GetPool().Exec(ctx, `INSERT INTO org_container_keys
		(org_id, cluster_uuid, namespace, workload, workload_type, container_name, last_reported, resolved_tags)
		VALUES ($1, $2, 'ns-rs', 'wl-rs-b', 'Deployment', 'ctr-rs-b', now(), $3::jsonb),
		       ($1, $4, 'ns-rs', 'wl-rs-a', 'Deployment', 'ctr-rs-a', now(), $5::jsonb)`,
		testutil.TestOrgID, readerClusterB, fmt.Sprintf(`{"%s": "x"}`, v16BOnlyTagKey),
		testutil.TestClusterUUID, fmt.Sprintf(`{"%s": "y"}`, v16AOnlyTagKey))
	require.NoError(t, err)

	// The tag-filter join needs the tenant tag table (per-org schema), as in
	// the tags integration tests.
	schema := strings.ReplaceAll(testutil.TestOrgID, "-", "_")
	_, err = database.GetPool().Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS `+schema+`;
		CREATE TABLE IF NOT EXISTS `+schema+`.reporting_ocptags_values (
			uuid UUID PRIMARY KEY,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			cluster_ids TEXT[] NOT NULL,
			cluster_aliases TEXT[] NOT NULL DEFAULT '{}',
			namespaces TEXT[] NOT NULL
		)`)
	require.NoError(t, err)
}

// v16WarningResponse is the per-route response shape carrying meta.warnings.
type v16WarningResponse struct {
	Meta struct {
		Warnings []string `json:"warnings"`
	} `json:"meta"`
	Warnings []string `json:"warnings"`
}

// v16GetRequests requests both tag keys on one route under one identity and
// returns the two bodies.
func v16GetRequests(t *testing.T, app *echo.Echo, path string) (bOnlyBody, noSuchBody string) {
	t.Helper()
	bodies := map[string]string{}
	for _, key := range []string{v16BOnlyTagKey, v16NoSuchTagKey} {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s?filter[tag:%s]=x", path, key), nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "route %s key %s body: %s", path, key, rec.Body.String())
		bodies[key] = rec.Body.String()
	}
	return bodies[v16BOnlyTagKey], bodies[v16NoSuchTagKey]
}

func v16Warns(body string) bool {
	return strings.Contains(body, "not in the known tag catalog")
}

// v16Routes registers every warning-attaching route under synthetic paths so
// the matrix does not depend on real route naming.
func v16Routes(v1 *echo.Group) {
	v1.GET("/tw/nodes", api.GetNodeUtilizationRecs)
	v1.GET("/tw/mig", api.GetGPUMIGRecommendations)
	v1.GET("/tw/history", api.GetRecommendationHistory)
	v1.GET("/tw/containers", api.GetNativeRecommendationSetList)
	// The production container registration (server.go) goes through the
	// fallback handler → serveNativeList — a different attach site.
	v1.GET("/tw/containers-fallback", api.GetRecommendationSetListWithFallback)
	v1.GET("/tw/namespaces", api.GetNamespaceRecommendationSetListWithFallback)
	v1.GET("/tw/pvcs", api.GetPVCRecommendations)
}

// TestTagWarningClusterScope_NoCrossClusterKeyOracle is #828's matrix: every
// restricted identity × route combination must treat a B-only observed key
// and a nonexistent key identically. Baseline: the data-derived catalog ran
// org-wide on the unscoped routes, so the B-only key suppressed its warning
// while the nonexistent key warned — the existence oracle.
func TestTagWarningClusterScope_NoCrossClusterKeyOracle(t *testing.T) {
	// Force-enable tags explicitly: a ROS_TAGS_ENABLED=false in the ambient
	// environment must not fail (or silently skip) this matrix (#828
	// review finding — the old require/skip pattern was env-dependent).
	t.Setenv("ROS_TAGS_ENABLED", "true")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)
	setupEligibilityTest(t)
	ctx := context.Background()
	seedReaderScopeOrg(t, ctx)
	seedV16TagKeys(t, ctx)
	require.True(t, config.TagsFeatureEnabled(), "tag feature must be force-enabled")

	cases := []struct {
		name  string
		perms map[string][]string
		path  string
	}{
		// The five unscoped caller groups (V16-01's finding).
		{"node-utilization", readerClusterAPerms(), "/api/cost-management/v1/tw/nodes"},
		{"gpu-mig", readerClusterAPerms(), "/api/cost-management/v1/tw/mig"},
		{"gpu-mig-empty-grant", map[string][]string{"openshift.cluster": {}}, "/api/cost-management/v1/tw/mig"},
		{"history", readerClusterAPerms(), "/api/cost-management/v1/tw/history"},
		{"native-container", readerClusterAPerms(), "/api/cost-management/v1/tw/containers"},
		{"container-fallback", readerClusterAPerms(), "/api/cost-management/v1/tw/containers-fallback"},
		{"namespace", readerClusterAPerms(), "/api/cost-management/v1/tw/namespaces"},
		// The nil-vs-empty conflation defeats even the scoped PVC path for
		// denied identities (filterClustersByRBAC returns nil; the warning
		// scope read nil as unscoped).
		{"pvc-empty-grant", map[string][]string{"openshift.cluster": {}}, "/api/cost-management/v1/tw/pvcs"},
		{"pvc-settings-only", map[string][]string{"settings.read": {"*"}}, "/api/cost-management/v1/tw/pvcs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := permsApp(tc.perms, v16Routes)
			bOnly, noSuch := v16GetRequests(t, app, tc.path)
			// Same treatment AND live warnings: both keys must WARN. Equality
			// alone would pass if a route suppressed all warnings (vacuous
			// green — #828 review finding); the nonzero-key warning must
			// still fire so the diagnostic stays useful post-fix.
			assert.True(t, v16Warns(bOnly),
				"restricted caller: the B-only key must warn as unknown (scoped catalog)")
			assert.True(t, v16Warns(noSuch),
				"restricted caller: the nonexistent key must warn (warning functionality intact)")
			// Pre-fix oracle, for the record: bOnly suppressed (key "known"
			// org-wide) while noSuch warned.
		})
	}
}

// TestTagWarningClusterScope_Controls pins the correct behaviors that must
// hold before AND after the fix.
func TestTagWarningClusterScope_Controls(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	seedReaderScopeOrg(t, ctx)
	seedV16TagKeys(t, ctx)
	require.True(t, config.TagsFeatureEnabled())

	t.Run("pvc-cluster-restricted-already-scoped", func(t *testing.T) {
		// #752's shipped scoping on the one wired route: A-only treats the
		// B-only key as unknown (both warn). Must not regress.
		app := permsApp(readerClusterAPerms(), v16Routes)
		bOnly, noSuch := v16GetRequests(t, app, "/api/cost-management/v1/tw/pvcs")
		assert.True(t, v16Warns(bOnly), "A-only must warn on the B-only key (scoped catalog)")
		assert.True(t, v16Warns(noSuch), "A-only must warn on a nonexistent key")
	})

	t.Run("wildcard-sees-org-wide-keys", func(t *testing.T) {
		// A wildcard user is org-authorized: the B-only key is legitimately
		// known (no warning) while the nonexistent key warns. This is
		// correct differential behavior, not an oracle.
		app := permsApp(map[string][]string{"*": {}}, v16Routes)
		for _, path := range []string{"/api/cost-management/v1/tw/nodes", "/api/cost-management/v1/tw/pvcs"} {
			bOnly, noSuch := v16GetRequests(t, app, path)
			assert.False(t, v16Warns(bOnly), "wildcard: org-wide B-only key is known (%s)", path)
			assert.True(t, v16Warns(noSuch), "wildcard: nonexistent key warns (%s)", path)
		}
	})

	t.Run("authorized-key-no-unknown-warning", func(t *testing.T) {
		// A key observed WITHIN the caller's scope must not produce the
		// unknown-key warning (the warning stays useful post-fix).
		app := permsApp(readerClusterAPerms(), v16Routes)
		req := httptest.NewRequest(http.MethodGet,
			fmt.Sprintf("/api/cost-management/v1/tw/pvcs?filter[tag:%s]=x", v16AOnlyTagKey), nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var resp v16WarningResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		for _, w := range allWarnings(resp) {
			assert.NotContains(t, w, v16AOnlyTagKey,
				"a key observed within the caller's scope must not warn as unknown")
		}
	})
}

// allWarnings merges the two warning locations routes use (meta.warnings,
// and the mirrored top-level warnings on node/GPU shapes).
func allWarnings(resp v16WarningResponse) []string {
	out := append([]string{}, resp.Meta.Warnings...)
	return append(out, resp.Warnings...)
}

// TestTagWarningClusterScope_BranchCoverage pins the attach sites and branch
// shapes the plain-route matrix cannot reach (#828 review finding): the
// namespace projection branch (a different attach site in
// serveNativeNamespaceList), and GPU MIG's client-filter-miss empty response
// (an A-only user filtering FOR cluster B — the client filter must narrow
// data, never the warning scope).
func TestTagWarningClusterScope_BranchCoverage(t *testing.T) {
	t.Setenv("ROS_TAGS_ENABLED", "true")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)
	setupEligibilityTest(t)
	ctx := context.Background()
	seedReaderScopeOrg(t, ctx)
	seedV16TagKeys(t, ctx)
	require.True(t, config.TagsFeatureEnabled())

	t.Run("namespace-projection-branch", func(t *testing.T) {
		// A term filter triggers hasListProjectionParams, taking the
		// projection attach site instead of the detail-list one.
		app := permsApp(readerClusterAPerms(), v16Routes)
		bodies := map[string]string{}
		for _, key := range []string{v16BOnlyTagKey, v16NoSuchTagKey} {
			req := httptest.NewRequest(http.MethodGet,
				fmt.Sprintf("/api/cost-management/v1/tw/namespaces?filter[term]=short&filter[tag:%s]=x", key), nil)
			req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
			bodies[key] = rec.Body.String()
		}
		assert.True(t, v16Warns(bodies[v16BOnlyTagKey]),
			"projection branch: B-only key must warn as unknown to an A-only user")
		assert.True(t, v16Warns(bodies[v16NoSuchTagKey]))
	})

	t.Run("gpu-mig-client-filter-miss", func(t *testing.T) {
		// A-only filtering FOR cluster B: restrictClustersToQueryFilter
		// misses → emptyGPUMIGResponse. The warning scope is still the
		// caller's A-only RBAC set (the client filter composes with, never
		// replaces, the server scope) — both keys must warn as unknown.
		app := permsApp(readerClusterAPerms(), v16Routes)
		bodies := map[string]string{}
		for _, key := range []string{v16BOnlyTagKey, v16NoSuchTagKey} {
			req := httptest.NewRequest(http.MethodGet,
				fmt.Sprintf("/api/cost-management/v1/tw/mig?filter[cluster]=%s&filter[tag:%s]=x", readerClusterB, key), nil)
			req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
			bodies[key] = rec.Body.String()
		}
		assert.True(t, v16Warns(bodies[v16BOnlyTagKey]),
			"client-filter-miss: the B-only key must warn — a client filter is not authorization and must not widen the warning scope")
		assert.True(t, v16Warns(bodies[v16NoSuchTagKey]))
	})
}
