package api_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/gpu"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// seedMIGRecommendationWorkloads seeds underutilized A100 workloads that receive
// non-full_gpu MIG profile recommendations (see engine/gpu_mig_integration_test.go).
func seedMIGRecommendationWorkloads(t *testing.T, pool *pgxpool.Pool, clusterUUID string, workloads []struct {
	ns, wl, cn, node string
}) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES (1, $1, 'mig-test-cluster', 'src-mig', now()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for _, wl := range workloads {
		for day := 0; day < 7; day++ {
			testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
				IntervalStart:       start.AddDate(0, 0, day),
				ClusterUUID:         clusterUUID,
				Namespace:           wl.ns,
				Workload:            wl.wl,
				WorkloadType:        "deployment",
				ContainerName:       wl.cn,
				GPUModelName:        "NVIDIA A100-SXM4-40GB",
				NodeName:            wl.node,
				FBUsageMinMiB:       400,
				FBUsageMaxMiB:       1200,
				FBUsageAvgMiB:       800,
				TensorPipeActiveMin: 0.02,
				TensorPipeActiveMax: 0.12,
				TensorPipeActiveAvg: 0.08,
				DRAMActiveMin:       0.05,
				DRAMActiveMax:       0.20,
				DRAMActiveAvg:       0.12,
				SMActiveMin:         0.08,
				SMActiveMax:         0.18,
				SMActiveAvg:         0.12,
			})
		}
	}
}

func setupGPUMIGEcho(pool *pgxpool.Pool) *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/gpu/mig", api.GetGPUMIGRecommendations)
	return app
}

func setupGPUMIGEchoWithRBAC(t *testing.T, pool *pgxpool.Pool, perms map[string][]string) *echo.Echo {
	t.Helper()
	cfg := config.GetConfig()
	orig := cfg.RBACEnabled
	cfg.RBACEnabled = true
	t.Cleanup(func() { cfg.RBACEnabled = orig })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user.permissions", perms)
			return next(c)
		}
	})
	v1.GET("/recommendations/openshift/gpu/mig", api.GetGPUMIGRecommendations)
	return app
}

func migListGET(t *testing.T, app *echo.Echo, query string) model.GPUMIGListResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/mig"+query, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp model.GPUMIGListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

func TestGetGPUMIGRecommendations_FilterCluster(t *testing.T) {
	const otherCluster = "22222222-2222-2222-2222-222222222222"

	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"mig-ns-a", "wl-a", "ctr-a", "gpu-node-a"},
	})
	seedMIGRecommendationWorkloads(t, pool, otherCluster, []struct {
		ns, wl, cn, node string
	}{
		{"mig-ns-b", "wl-b", "ctr-b", "gpu-node-b"},
	})

	app := setupGPUMIGEcho(pool)
	all := migListGET(t, app, "")
	require.Greater(t, all.Meta.Count, 1, "need rows on both clusters")

	filtered := migListGET(t, app, "?filter%5Bcluster%5D="+testutil.TestClusterUUID)
	require.Greater(t, filtered.Meta.Count, 0)
	for _, row := range filtered.Data {
		assert.Equal(t, testutil.TestClusterUUID, row.ClusterUUID)
	}
}

func TestGetGPUMIGRecommendations_FilterClusterMissReturnsEmpty(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"mig-ns-a", "wl-a", "ctr-a", "gpu-node-a"},
	})

	app := setupGPUMIGEcho(pool)
	resp := migListGET(t, app, "?filter%5Bcluster%5D=99999999-9999-9999-9999-999999999999")
	assert.Equal(t, 0, resp.Meta.Count)
	assert.Empty(t, resp.Data)
}

func TestGetGPUMIGRecommendations_FilterProject(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"target-mig-ns", "wl-1", "ctr-1", "gpu-node-1"},
		{"other-mig-ns", "wl-2", "ctr-2", "gpu-node-1"},
	})

	app := setupGPUMIGEcho(pool)
	resp := migListGET(t, app, "?filter%5Bproject%5D=target-mig-ns")
	require.Greater(t, resp.Meta.Count, 0)
	for _, row := range resp.Data {
		assert.Equal(t, "target-mig-ns", row.Namespace)
	}
}

func TestGetGPUMIGRecommendations_FilterProjectNamespaceAlias(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"alias-mig-ns", "wl-1", "ctr-1", "gpu-node-1"},
		{"skip-mig-ns", "wl-2", "ctr-2", "gpu-node-1"},
	})

	app := setupGPUMIGEcho(pool)
	resp := migListGET(t, app, "?filter%5Bnamespace%5D=alias-mig-ns")
	require.Greater(t, resp.Meta.Count, 0)
	for _, row := range resp.Data {
		assert.Equal(t, "alias-mig-ns", row.Namespace)
	}
}

func TestGetGPUMIGRecommendations_FilterTermNormalization(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"term-norm-ns", "wl-term", "ctr-term", "gpu-node-term"},
	})

	app := setupGPUMIGEcho(pool)

	unfiltered := migListGET(t, app, "?limit=100")
	require.Greater(t, unfiltered.Meta.Count, 0, "need data for term filter test")

	tests := []struct {
		name  string
		query string
	}{
		{"short_term bracket", "?filter%5Bterm%5D=short_term"},
		{"short_term flat", "?term=short_term"},
		{"short bracket", "?filter%5Bterm%5D=short"},
		{"short flat", "?term=short"},
		{"SHORT_TERM uppercase", "?filter%5Bterm%5D=SHORT_TERM"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := migListGET(t, app, tc.query)
			require.Greater(t, resp.Meta.Count, 0, "term filter %q should return results", tc.query)
			for _, row := range resp.Data {
				assert.Equal(t, "short", row.Term, "all rows should have short term")
			}
		})
	}

	badReq := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/mig?filter%5Bterm%5D=invalid_value", nil)
	badReq.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	badRec := httptest.NewRecorder()
	app.ServeHTTP(badRec, badReq)
	assert.Equal(t, http.StatusBadRequest, badRec.Code)
}

func TestGetGPUMIGRecommendations_UnsupportedOrderByConfidence(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupGPUMIGEcho(pool)
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/mig?order_by=confidence&order_how=desc", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetGPUMIGRecommendations_InvalidOrderBy(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupGPUMIGEcho(pool)
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/mig?order_by=estimated_monthly_gpu_savings_usd", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetGPUMIGRecommendations_FormatCSV(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"csv-mig-ns", "wl-csv", "ctr-csv", "gpu-node-csv"},
	})

	app := setupGPUMIGEcho(pool)
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/mig?format=csv&limit=100", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/csv")

	reader := csv.NewReader(strings.NewReader(rec.Body.String()))
	rows, err := reader.ReadAll()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 2)
	assert.Equal(t, "cluster_uuid", rows[0][0])
	assert.Equal(t, "namespace", rows[0][1])
	assert.Contains(t, rows[0], "gpu_classification")
}

func TestGetGPUMIGRecommendations_FilterTag(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "api")
	require.True(t, config.TagsFeatureEnabled())

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"tagged-mig-ns", "wl-tagged", "ctr-tagged", "gpu-node-tag"},
		{"untagged-mig-ns", "wl-other", "ctr-other", "gpu-node-tag"},
	})

	_, err := pool.Exec(ctx, `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
		VALUES ($1, $2, 'tagged-mig-ns', 'wl-tagged', 'deployment', 'ctr-tagged', '{"environment":"production"}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupGPUMIGEcho(pool)
	resp := migListGET(t, app, "?filter%5Btag%3Aenvironment%5D=production")
	require.Greater(t, resp.Meta.Count, 0)
	for _, row := range resp.Data {
		assert.Equal(t, "tagged-mig-ns", row.Namespace)
	}
}

func TestGetGPUMIGRecommendations_FilterTagIgnoredWhenDisabled(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "false")
	require.False(t, config.TagsFeatureEnabled())

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"tagged-mig-ns", "wl-tagged", "ctr-tagged", "gpu-node-tag"},
		{"untagged-mig-ns", "wl-other", "ctr-other", "gpu-node-tag"},
	})

	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
		VALUES ($1, $2, 'tagged-mig-ns', 'wl-tagged', 'deployment', 'ctr-tagged', '{"environment":"production"}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupGPUMIGEcho(pool)
	resp := migListGET(t, app, "?filter%5Btag%3Aenvironment%5D=production")
	assert.GreaterOrEqual(t, resp.Meta.Count, 2, "tag filter must be ignored when ROS_TAGS_ENABLED=false")
}

func TestGetGPUMIGRecommendations_RBAC_FiltersByCluster(t *testing.T) {
	const cluster1 = "c1111111-1111-1111-1111-111111111111"
	const cluster2 = "c2222222-2222-2222-2222-222222222222"

	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, cluster1, []struct {
		ns, wl, cn, node string
	}{
		{"rbac-mig-ns", "wl-1", "ctr-1", "gpu-node-c1"},
	})
	seedMIGRecommendationWorkloads(t, pool, cluster2, []struct {
		ns, wl, cn, node string
	}{
		{"rbac-mig-ns-2", "wl-2", "ctr-2", "gpu-node-c2"},
	})

	app := setupGPUMIGEchoWithRBAC(t, pool, map[string][]string{
		"openshift.cluster": {cluster1},
		"openshift.node":    {"*"},
	})

	resp := migListGET(t, app, "")
	require.Greater(t, resp.Meta.Count, 0)
	for _, row := range resp.Data {
		assert.Equal(t, cluster1, row.ClusterUUID)
	}
}

func TestGetGPUMIGRecommendations_RBAC_UnauthorizedClusterFilterEmpty(t *testing.T) {
	const allowed = "c1111111-1111-1111-1111-111111111111"
	const denied = "c2222222-2222-2222-2222-222222222222"

	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, allowed, []struct {
		ns, wl, cn, node string
	}{
		{"rbac-mig-ns", "wl-1", "ctr-1", "gpu-node-c1"},
	})
	seedMIGRecommendationWorkloads(t, pool, denied, []struct {
		ns, wl, cn, node string
	}{
		{"rbac-mig-ns-2", "wl-2", "ctr-2", "gpu-node-c2"},
	})

	app := setupGPUMIGEchoWithRBAC(t, pool, map[string][]string{
		"openshift.cluster": {allowed},
		"openshift.node":    {"*"},
	})

	resp := migListGET(t, app, "?filter%5Bcluster%5D="+denied)
	assert.Equal(t, 0, resp.Meta.Count)
	assert.Empty(t, resp.Data)
}

func TestGetGPUMIGRecommendations_Unauthorized(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupGPUMIGEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/mig", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func setupGPUMIGEchoWithSettings(pool *pgxpool.Pool) *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/gpu/mig", api.GetGPUMIGRecommendations)
	api.RegisterThresholdSettingsRoutes(v1)
	return app
}

func TestGPUMIGRecommendations_Pagination(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"mig-pag-a", "wl-a", "ctr-a", "gpu-node-1"},
		{"mig-pag-b", "wl-b", "ctr-b", "gpu-node-1"},
		{"mig-pag-c", "wl-c", "ctr-c", "gpu-node-1"},
	})

	app := setupGPUMIGEcho(pool)
	all := migListGET(t, app, "?limit=100")
	require.GreaterOrEqual(t, all.Meta.Count, 3, "need multiple MIG keys for pagination")

	// limit=1 means 1 SQL key; each key may produce multiple entries (one per term).
	page0 := migListGET(t, app, "?limit=1&order_by=namespace&order_how=asc")
	assert.Equal(t, all.Meta.Count, page0.Meta.Count, "count should be consistent")
	assert.Equal(t, 1, page0.Meta.Limit)
	require.Greater(t, len(page0.Data), 0, "first page should have entries")
	require.True(t, page0.Meta.HasNext, "expected has_next when more keys exist than limit")
	require.NotEmpty(t, page0.Meta.NextCursor)

	page1 := migListGET(t, app, "?limit=1&order_by=namespace&order_how=asc&after="+url.QueryEscape(page0.Meta.NextCursor))
	require.Greater(t, len(page1.Data), 0, "second page should have entries")
	assert.NotEqual(t, page0.Data[0].Namespace, page1.Data[0].Namespace,
		"keyset pagination should advance to a different SQL key")
}

func TestGPUMIGRecommendations_SQLPagination_MultiPage(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"sql-pag-a", "wl-a", "ctr-a", "gpu-node-1"},
		{"sql-pag-b", "wl-b", "ctr-b", "gpu-node-1"},
		{"sql-pag-c", "wl-c", "ctr-c", "gpu-node-1"},
		{"sql-pag-d", "wl-d", "ctr-d", "gpu-node-1"},
		{"sql-pag-e", "wl-e", "ctr-e", "gpu-node-1"},
	})

	app := setupGPUMIGEcho(pool)

	all := migListGET(t, app, "?limit=100&order_by=namespace&order_how=asc")
	require.GreaterOrEqual(t, all.Meta.Count, 5, "need 5+ MIG keys for multi-page test")

	seen := make(map[string]bool)
	var allData []model.GPUMIGRecommendationEntry
	cursor := ""
	pages := 0
	for {
		query := "?limit=2&order_by=namespace&order_how=asc"
		if cursor != "" {
			query += "&after=" + url.QueryEscape(cursor)
		}
		page := migListGET(t, app, query)

		for _, row := range page.Data {
			key := row.ClusterUUID + "/" + row.Namespace + "/" + row.Container + "/" + row.GPUModel + "/" + row.Term
			assert.False(t, seen[key], "duplicate row across pages: %s", key)
			seen[key] = true
		}
		allData = append(allData, page.Data...)
		pages++

		if !page.Meta.HasNext {
			break
		}
		require.NotEmpty(t, page.Meta.NextCursor)
		cursor = page.Meta.NextCursor
		require.Less(t, pages, 50, "safety: too many pages")
	}
	assert.GreaterOrEqual(t, len(allData), 5, "all pages combined should have at least 5 entries")
}

func TestGPUMIGRecommendations_SQLPagination_EmptyResult(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupGPUMIGEcho(pool)
	resp := migListGET(t, app, "?limit=10&order_by=namespace")
	assert.Equal(t, 0, resp.Meta.Count)
	assert.Empty(t, resp.Data)
	assert.False(t, resp.Meta.HasNext)
	assert.Empty(t, resp.Meta.NextCursor)
}

func TestGPUMIGRecommendations_SQLPagination_SortColumns(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"sort-ns-b", "wl-b", "ctr-b", "gpu-node-1"},
		{"sort-ns-a", "wl-a", "ctr-a", "gpu-node-1"},
		{"sort-ns-c", "wl-c", "ctr-c", "gpu-node-1"},
	})

	app := setupGPUMIGEcho(pool)

	for _, col := range []string{"cluster_uuid", "namespace", "workload", "container", "gpu_model"} {
		t.Run(col, func(t *testing.T) {
			page := migListGET(t, app, "?limit=2&order_by="+col+"&order_how=asc")
			require.Greater(t, len(page.Data), 0, "should return data for order_by=%s", col)
			assert.True(t, page.Meta.HasNext || page.Meta.Count <= 2,
				"has_next should be true when more keys exist than limit")

			if page.Meta.HasNext {
				require.NotEmpty(t, page.Meta.NextCursor)
				page2 := migListGET(t, app, "?limit=2&order_by="+col+"&order_how=asc&after="+url.QueryEscape(page.Meta.NextCursor))
				require.Greater(t, len(page2.Data), 0, "second page should have data")
			}
		})
	}
}

func TestGPUMIGRecommendations_SQLPagination_CountIsApproximate(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"count-ns-a", "wl-a", "ctr-a", "gpu-node-1"},
		{"count-ns-b", "wl-b", "ctr-b", "gpu-node-1"},
	})

	app := setupGPUMIGEcho(pool)

	page := migListGET(t, app, "?limit=1&order_by=namespace")
	assert.GreaterOrEqual(t, page.Meta.Count, 2,
		"count should reflect SQL-level key count (approximate, pre-filter)")
	assert.Equal(t, page.Meta.Count, migListGET(t, app, "?limit=100&order_by=namespace").Meta.Count,
		"count should be consistent across pages")
}

func TestGPUMIGRecommendations_SQLPagination_DescOrder(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"desc-ns-a", "wl-a", "ctr-a", "gpu-node-1"},
		{"desc-ns-b", "wl-b", "ctr-b", "gpu-node-1"},
		{"desc-ns-c", "wl-c", "ctr-c", "gpu-node-1"},
	})

	app := setupGPUMIGEcho(pool)

	// limit=1 means 1 SQL key, which may produce multiple entries (one per term).
	page0 := migListGET(t, app, "?limit=1&order_by=namespace&order_how=desc")
	require.Greater(t, len(page0.Data), 0)
	require.True(t, page0.Meta.HasNext)
	page0NS := page0.Data[0].Namespace

	page1 := migListGET(t, app, "?limit=1&order_by=namespace&order_how=desc&after="+url.QueryEscape(page0.Meta.NextCursor))
	require.Greater(t, len(page1.Data), 0)
	page1NS := page1.Data[0].Namespace
	assert.Greater(t, page0NS, page1NS,
		"DESC order: first page namespace should be greater than second page")
}

func TestGPUMIGRecommendations_FilterIdleState(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{"mig-idle-ns", "wl-idle", "ctr-idle", "gpu-node-idle"},
		{"mig-active-ns", "wl-active", "ctr-active", "gpu-node-active"},
	})

	app := setupGPUMIGEcho(pool)
	unfiltered := migListGET(t, app, "?limit=100")
	require.Greater(t, unfiltered.Meta.Count, 0)

	activeFiltered := migListGET(t, app, "?filter%5Bgpu_idle_state%5D=active&limit=100")
	require.Greater(t, activeFiltered.Meta.Count, 0)
	assert.LessOrEqual(t, activeFiltered.Meta.Count, unfiltered.Meta.Count)
	for _, row := range activeFiltered.Data {
		assert.Equal(t, "active", row.GPUIdleState)
	}

	idleFiltered := migListGET(t, app, "?filter%5Bgpu_idle_state%5D=idle&limit=100")
	assert.LessOrEqual(t, idleFiltered.Meta.Count, unfiltered.Meta.Count)
	for _, row := range idleFiltered.Data {
		assert.Equal(t, "idle", row.GPUIdleState)
	}
}

func TestGPUMIGRecommendations_Settings(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupGPUMIGEchoWithSettings(pool)
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/gpu", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.02, resp["idle_threshold"].(float64), 1e-9)
	assert.InDelta(t, 0.25, resp["underutilized_sm_threshold"].(float64), 1e-9)
	assert.InDelta(t, 1.20, resp["fb_headroom_factor"].(float64), 1e-9)
	assert.InDelta(t, 0.98, resp["mig_fb_percentile"].(float64), 1e-9)
}

func seedContainerDigestsForMIGContainer(
	t *testing.T,
	pool *pgxpool.Pool,
	start time.Time,
	days int,
	namespace, workload, container string,
) {
	t.Helper()
	for i := 0; i < days; i++ {
		cpuVal := int64(200 + i*10)
		memVal := int64(524288 + i*1024)
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:       start.AddDate(0, 0, i),
			OrgID:            testutil.TestOrgID,
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        namespace,
			Workload:         workload,
			WorkloadType:     "deployment",
			ContainerName:    container,
			CPURequestP50MC:  cpuVal - 20,
			CPURequestP95MC:  cpuVal + 10,
			CPUUsageP50MC:    cpuVal - 10,
			CPUUsageP95MC:    cpuVal,
			CPUUsageP98MC:    cpuVal + 5,
			CPUUsageP99MC:    cpuVal + 8,
			CPUUsageMaxMC:    cpuVal + 15,
			CPUThrottleP95MC: 5,
			CPUThrottleMaxMC: 10,
			MemRequestP50KiB: memVal - 1024,
			MemRequestP60KiB: memVal - 512,
			MemRequestP95KiB: memVal + 512,
			MemRequestP98KiB: memVal + 768,
			MemRequestP99KiB: memVal + 900,
			MemUsageP50KiB:   memVal - 512,
			MemUsageP60KiB:   memVal,
			MemUsageP95KiB:   memVal + 512,
			MemUsageP98KiB:   memVal + 768,
			MemUsageP99KiB:   memVal + 900,
			MemUsageMaxKiB:   memVal + 1024,
			MemRSSP95KiB:     memVal - 256,
			MemRSSMaxKiB:     memVal + 512,
			OOMCountSum:      0,
			CPUUsageMeanMC:   cpuVal - 5,
			MemUsageMeanKiB:  memVal - 256,
			SampleCount:      96,
		})
	}
}

func TestGPUMIGRecommendations_NotificationCodes(t *testing.T) {
	const (
		migNS  = "mig-notif-ns"
		migWL  = "wl-notif"
		migCtr = "ctr-notif"
	)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES (1, $1, 'mig-notif-cluster', 'src-mig-notif', now()) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	seedContainerDigestsForMIGContainer(t, pool, start, 7, migNS, migWL, migCtr)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{
		{migNS, migWL, migCtr, "gpu-node-notif"},
	})
	require.NoError(t, engine.MarkContainersWithGPU(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID))
	require.NoError(t, engine.StoreGPUClassifications(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, engine.DefaultTerms(), nil))

	migList := migListGET(t, setupGPUMIGEcho(pool), "?filter%5Bproject%5D="+migNS+"&limit=10")
	require.Greater(t, migList.Meta.Count, 0)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/:recommendation-id", api.GetNativeRecommendationSet)

	containerID := model.NativeContainerID(
		testutil.TestClusterUUID, migNS, migWL, "deployment", migCtr,
	)
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/"+containerID, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var detail model.DetailResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
	require.NotEmpty(t, detail.GPU)

	migCodes := map[int16]bool{
		engine.NotifGPUUnderutilized:   true,
		engine.NotifGPUIdle:            true,
		engine.NotifGPUMemBound:        true,
		engine.NotifGPUNoProfilingData: true,
	}
	found := false
	for _, gpuRec := range detail.GPU {
		for _, code := range gpuRec.Notifications {
			if migCodes[code] {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "container detail gpu.*.notifications should include MIG advisory codes 10, 26, 27, or 28")
}

func TestGPUMIGRecommendations_PluginDisabled(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	config.ResetForTest()
	_ = config.GetConfig()

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	api.RegisterV1RoutesForTest(v1, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/mig", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "not_found", body["status"])
	msg, ok := body["message"].(string)
	require.True(t, ok)
	require.Contains(t, msg, "plugin 'gpu' is not enabled")
}
