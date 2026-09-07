package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// The legacy handler has no CSV export (#536): requesting a CSV format must
// fail closed with 406. This pins the legacy/Kruize route, which production
// still serves directly when the Kruize flag is on
// (plugins/namespace/plugin.go) — it says nothing about the default native
// route, covered below. Empty tables still reach the format switch, so no
// seeds are needed.
func TestGetNamespaceRecommendationSetList_CSVFormat_Returns406(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/namespaces", api.GetNamespaceRecommendationSetListWithFallback)

	for _, tc := range []struct {
		name   string
		header map[string]string
	}{
		{"accept header", map[string]string{"Accept": "text/csv"}},
		{"format param", map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := "/api/cost-management/v1/recommendations/openshift/namespaces"
			if tc.name == "format param" {
				path += "?format=csv"
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			require.Equal(t, http.StatusNotAcceptable, rec.Code, rec.Body.String())

			var body map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, "error", body["status"])
		})
	}
}

// seedNativeNamespace builds one namespace with recommendations through the
// established chain (same as the WithFallback integration tests): digest
// series → engine recs → persisted recs → refreshed org keys.
func seedNativeNamespace(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'ns-csv-cluster', 'src-ns-csv', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	testutil.SeedNamespaceDigestSeries(t, pool, "ns-csv", 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := engine.RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.NoError(t, engine.WriteNamespaceRecommendations(ctx, pool, results))
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, testutil.TestOrgID))
}

// mountNamespaceFallback mounts the production default route: native first,
// legacy fallback when native returns no rows.
func mountNamespaceFallback() *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/namespaces", api.GetNamespaceRecommendationSetListWithFallback)
	return app
}

// Empty native tables fall through to the legacy handler, so CSV still 406s
// there — this pins the fallback leg's routing, not the format switch.
func TestNamespaceFallback_EmptyDB_CSVFallsThroughTo406(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })
	app := mountNamespaceFallback()

	for _, tc := range []struct {
		name string
		path string
		hdr  map[string]string
	}{
		{"accept header", "/api/cost-management/v1/recommendations/openshift/namespaces", map[string]string{"Accept": "text/csv"}},
		{"format param", "/api/cost-management/v1/recommendations/openshift/namespaces?format=csv", map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
			for k, v := range tc.hdr {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			require.Equal(t, http.StatusNotAcceptable, rec.Code, rec.Body.String())
		})
	}
}

// Seeded native rows serve CSV 200 through the default route: routing the
// same request to the legacy handler instead would 406, naming the routing
// (not the format switch) on failure.
func TestNamespaceFallback_Seeded_CSVServes200(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })
	seedNativeNamespace(t, ctx, pool)
	app := mountNamespaceFallback()

	for _, tc := range []struct {
		name string
		path string
		hdr  map[string]string
	}{
		{"accept header", "/api/cost-management/v1/recommendations/openshift/namespaces", map[string]string{"Accept": "text/csv"}},
		{"format param", "/api/cost-management/v1/recommendations/openshift/namespaces?format=csv", map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
			for k, v := range tc.hdr {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Header().Get("Content-Type"), "text/csv")
			assert.NotEmpty(t, strings.TrimSpace(rec.Body.String()))
		})
	}
}
