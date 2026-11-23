package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// The category/classification contract (#358-#362, #914): the unified
// classification is served on list rows and filter[category] honors it.
// Valid values (see MapNativeQueryParameters / the namespace mapper):
// zombie, idle, undersized, oversized, optimized.

// seedCategorizedNamespace seeds one namespace whose recommendations carry a
// category, and fails the test loudly if the engine produced none (fixture
// drift guard — the assertions below are only meaningful with categorized
// rows in the DB).
func seedCategorizedNamespace(t *testing.T, namespace string) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, $2, 'src-cat', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID, "cat-cluster")
	require.NoError(t, err)

	testutil.SeedNamespaceDigestSeries(t, pool, namespace, 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := engine.RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.NoError(t, engine.WriteNamespaceRecommendations(ctx, pool, results))
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, testutil.TestOrgID))

	var categorized int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM namespace_recommendation_sets WHERE org_id = $1 AND category IS NOT NULL AND category <> ''`,
		testutil.TestOrgID).Scan(&categorized)
	require.NoError(t, err)
	require.Greater(t, categorized, 0, "fixture drift: seeded namespaces carry no category in the DB")
}

func newNamespaceListApp() *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/namespaces", api.GetNamespaceRecommendationSetListWithFallback)
	return app
}

type categoryListResponse struct {
	Data []struct {
		Category string `json:"category"`
	} `json:"data"`
	Meta struct {
		Count int `json:"count"`
	} `json:"meta"`
}

func TestNamespaceListServesCategory(t *testing.T) {
	seedCategorizedNamespace(t, "ns-category-serve")

	app := newNamespaceListApp()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/namespaces?limit=10&start_date="+testutil.BaseDate.Format("2006-01-02"), nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp categoryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data, "seeded namespace must appear in the list")

	// The default (no projection params) list path serves
	// NamespaceDetailResponse; the classification must survive that mapping
	// (#914: the slim projection path served it, the default path dropped it).
	for i, row := range resp.Data {
		assert.Contains(t, []string{"zombie", "idle", "undersized", "oversized", "optimized"},
			row.Category, "row %d carries no category: the default namespace list path drops the classification", i)
	}
}

// seedNamespaceDigestRow inserts a daily namespace digest row with request
// and usage set independently — the shared SeedNamespaceDigestSeries couples
// them (usage ≈ request), which cannot express divergent categories.
func seedNamespaceDigestRow(t *testing.T, pool *pgxpool.Pool, namespace string, requestMC, usageMC, requestKiB, usageKiB int64) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 7; i++ {
		bucketDate := testutil.BaseDate.AddDate(0, 0, i)
		testutil.EnsureMonthlyPartition(t, pool, "daily_namespace_digests", bucketDate)
		_, err := pool.Exec(ctx, `
			INSERT INTO daily_namespace_digests (
				bucket_date, org_id, cluster_uuid, namespace,
				cpu_request_p50_mc, cpu_request_p95_mc,
				cpu_usage_p50_mc, cpu_usage_p95_mc, cpu_usage_max_mc,
				memory_request_p50_kib, memory_request_p95_kib,
				memory_usage_p50_kib, memory_usage_p95_kib, memory_usage_max_kib,
				cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, $7, $8, $9,
				$10, $11, $12, $13, $14,
				$15, $16, $17
			) ON CONFLICT (org_id, cluster_uuid, namespace, bucket_date, schedule_type)
			DO UPDATE SET cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc`,
			bucketDate, testutil.TestOrgID, testutil.TestClusterUUID, namespace,
			requestMC, requestMC+10,
			usageMC, usageMC, usageMC+15,
			requestKiB, requestKiB+512,
			usageKiB, usageKiB, usageKiB+1024,
			usageMC, usageKiB, int64(96),
		)
		require.NoError(t, err)
	}
}

func TestNamespaceListFilterCategoryNarrows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'cat-narrow-cluster', 'src-cat-n', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	// Two namespaces with divergent request/usage ratios so the engine
	// classifies them differently — the narrowing assertion is only
	// meaningful with more than one category present.
	testutil.SeedNamespaceDigestSeries(t, pool, "ns-cat-shaped", 7, 200, 10, 524288, 1024)
	seedNamespaceDigestRow(t, pool, "ns-cat-oversized", 1000, 100, 1048576, 131072)

	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := engine.RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.NoError(t, engine.WriteNamespaceRecommendations(ctx, pool, results))
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, testutil.TestOrgID))

	var categories []string
	catRows, err := pool.Query(ctx,
		`SELECT DISTINCT category FROM namespace_recommendation_sets WHERE org_id = $1 AND category IS NOT NULL AND category <> ''`,
		testutil.TestOrgID)
	require.NoError(t, err)
	defer catRows.Close()
	for catRows.Next() {
		var c string
		require.NoError(t, catRows.Scan(&c))
		categories = append(categories, c)
	}
	require.GreaterOrEqual(t, len(categories), 2, "fixture drift: expected two distinct categories, got %v", categories)
	target := categories[0]

	app := newNamespaceListApp()
	base := "/api/cost-management/v1/recommendations/openshift/namespaces?limit=10&start_date=" + testutil.BaseDate.Format("2006-01-02")

	get := func(query string) categoryListResponse {
		req := httptest.NewRequest(http.MethodGet, query, nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var resp categoryListResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp
	}

	unfiltered := get(base)
	filtered := get(base + "&filter%5Bcategory%5D=" + target)

	assert.Less(t, filtered.Meta.Count, unfiltered.Meta.Count,
		"filter[category]=%s must narrow the result set", target)
	for i, row := range filtered.Data {
		assert.Equal(t, target, row.Category, "filtered row %d must carry the filtered category", i)
	}
}

func TestNamespaceListFilterCategoryInvalidRejected(t *testing.T) {
	seedCategorizedNamespace(t, "ns-category-invalid")

	app := newNamespaceListApp()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/namespaces?limit=10&filter%5Bcategory%5D=not-a-category", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	// #841's mechanism, category dimension: an invalid filter value must be
	// rejected, not silently dropped to return the full unfiltered set.
	require.Equal(t, http.StatusBadRequest, rec.Code,
		"filter[category]=not-a-category must 400, got %d: %s", rec.Code, rec.Body.String())
}

func TestContainerListFilterCategoryInvalidRejected(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'cat-container-cluster', 'src-cat-c', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate: start.AddDate(0, 0, i),
			OrgID:      testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
			Namespace: "ns-cat-cont", Workload: "deploy", WorkloadType: "deployment",
			ContainerName:   "app",
			CPURequestP50MC: 100, CPURequestP95MC: 120,
			CPUUsageP50MC: 90, CPUUsageP95MC: 110, CPUUsageP98MC: 115,
			CPUUsageP99MC: 118, CPUUsageMaxMC: 125,
			CPUThrottleP95MC: 5, CPUThrottleMaxMC: 10,
			MemRequestP50KiB: 524288, MemRequestP95KiB: 524800,
			MemUsageP50KiB: 524000, MemUsageP95KiB: 524288,
			MemUsageMaxKiB: 525312, MemRSSP95KiB: 524000, MemRSSMaxKiB: 525000,
			OOMCountSum: 0, CPUUsageMeanMC: 95, MemUsageMeanKiB: 523000,
			SampleCount: 96,
		})
	}
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?limit=10&filter%5Bcategory%5D=not-a-category", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code,
		"filter[category]=not-a-category must 400, got %d: %s", rec.Code, rec.Body.String())
}
