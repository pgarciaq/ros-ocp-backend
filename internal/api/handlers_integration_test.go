package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func makeIdentityHeader(orgID string) string {
	id := map[string]interface{}{
		"identity": map[string]interface{}{
			"org_id":         orgID,
			"account_number": "test",
			"type":           "User",
			"user": map[string]interface{}{
				"username":     "test_user",
				"is_org_admin": true,
			},
		},
		"entitlements": map[string]interface{}{},
	}
	b, _ := json.Marshal(id)
	return base64.StdEncoding.EncodeToString(b)
}

func TestGetNativeRecommendationSetList_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB

	// Seed rh_accounts and clusters so the JOIN resolves
	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	// Seed digests with recent dates to avoid the 3-day staleness filter.
	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)

	err = engine.WriteRecommendations(ctx, pool, recs)
	require.NoError(t, err)

	// Set up Echo with identity middleware + native handler
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	// JSON response test
	t.Run("JSON response", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		var response struct {
			Data []model.DetailResponse `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.Greater(t, response.Meta.Count, 0, "should have results; body: %s", rec.Body.String())
		require.NotEmpty(t, response.Data, "data should not be empty")

		first := response.Data[0]
		assert.Equal(t, testutil.TestClusterUUID, first.ClusterUUID)
		assert.Equal(t, testutil.TestNamespace, first.Project)
		assert.NotEmpty(t, first.Recommendations.RecommendationTerms)
	})

	// CSV response test
	t.Run("CSV response", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?format=csv", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/csv")
		body := rec.Body.String()
		assert.Contains(t, body, "cluster_uuid")
		assert.Contains(t, body, testutil.TestClusterUUID)
	})

	// Missing identity test
	t.Run("missing identity returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift", nil)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	// Reset global
	database.DB = nil
}

func TestGetNativeRecommendationSetList_PaginationCount(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	defer func() { database.DB = nil }()

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	err = engine.WriteRecommendations(ctx, pool, recs)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data []model.DetailResponse `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, len(response.Data), response.Meta.Count,
		"meta.count should equal the number of distinct containers")
	assert.Greater(t, response.Meta.Count, 0)
}

func TestGetNativeRecommendationSet_DetailEndpoint(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	defer func() { database.DB = nil }()

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	err = engine.WriteRecommendations(ctx, pool, recs)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)
	v1.GET("/recommendations/openshift/:recommendation-id", api.GetNativeRecommendationSet)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	// First get the list to extract the ID
	t.Run("fetch detail by ID from list", func(t *testing.T) {
		listReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift", nil)
		listReq.Header.Set("X-Rh-Identity", identityHeader)
		listRec := httptest.NewRecorder()
		app.ServeHTTP(listRec, listReq)
		require.Equal(t, http.StatusOK, listRec.Code)

		var listResp struct {
			Data []model.DetailResponse `json:"data"`
		}
		err := json.Unmarshal(listRec.Body.Bytes(), &listResp)
		require.NoError(t, err)
		require.NotEmpty(t, listResp.Data)

		containerID := listResp.Data[0].ID
		assert.NotEmpty(t, containerID)

		// Fetch detail
		detailReq := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/"+containerID, nil)
		detailReq.Header.Set("X-Rh-Identity", identityHeader)
		detailRec := httptest.NewRecorder()
		app.ServeHTTP(detailRec, detailReq)
		require.Equal(t, http.StatusOK, detailRec.Code)

		var detail model.DetailResponse
		err = json.Unmarshal(detailRec.Body.Bytes(), &detail)
		require.NoError(t, err)

		assert.Equal(t, containerID, detail.ID)
		assert.Equal(t, testutil.TestClusterUUID, detail.ClusterUUID)
		assert.Equal(t, testutil.TestNamespace, detail.Project)
		assert.Equal(t, testutil.TestWorkload, detail.Workload)
		assert.Equal(t, testutil.TestContainer, detail.Container)
		assert.NotEmpty(t, detail.Recommendations.RecommendationTerms)
	})

	t.Run("bad UUID returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/not-a-uuid", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("nonexistent UUID returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/00000000-0000-0000-0000-000000000000", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("missing identity returns 401", func(t *testing.T) {
		containerID := model.NativeContainerID(testutil.TestClusterUUID, testutil.TestNamespace, testutil.TestWorkload, testutil.TestContainer)
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/"+containerID, nil)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestGetNativeRecommendationSetList_OrgIsolation(t *testing.T) {
	// T-2.2: Org A must not see org B's recommendations.
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	defer func() { database.DB = nil }()

	orgA := "orgAAAAAAAA"
	orgB := "orgBBBBBBBB"
	clusterA := "aaaa1111-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	clusterB := "bbbb2222-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	// Seed rh_accounts and clusters for both orgs
	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (100, $1), (200, $2) ON CONFLICT DO NOTHING`, orgA, orgB)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (100, $1, 'cluster-a', 'src-a', now()), (200, $2, 'cluster-b', 'src-b', now()) ON CONFLICT DO NOTHING`, clusterA, clusterB)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for i := 0; i < 7; i++ {
		for _, org := range []struct{ orgID, cluster, ns string }{
			{orgA, clusterA, "ns-a"},
			{orgB, clusterB, "ns-b"},
		} {
			testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
				BucketDate: start.AddDate(0, 0, i),
				OrgID:      org.orgID, ClusterUUID: org.cluster,
				Namespace: org.ns, Workload: "deploy", WorkloadType: "deployment",
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
	}

	end := start.AddDate(0, 0, 6)
	recsA, err := engine.RecommendAllWorkloads(ctx, pool, orgA, clusterA, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recsA)
	recsB, err := engine.RecommendAllWorkloads(ctx, pool, orgB, clusterB, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recsB)
	require.NoError(t, engine.WriteRecommendations(ctx, pool, recsA))
	require.NoError(t, engine.WriteRecommendations(ctx, pool, recsB))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	// Request as org A: should see only org A's data
	t.Run("org A sees only its own data", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift", nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgA))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var response struct {
			Data []model.DetailResponse `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.Greater(t, response.Meta.Count, 0)

		for _, d := range response.Data {
			assert.Equal(t, clusterA, d.ClusterUUID, "org A should only see cluster A")
			assert.Equal(t, "ns-a", d.Project, "org A should only see namespace ns-a")
		}
	})

	// Request as org B: should see only org B's data
	t.Run("org B sees only its own data", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift", nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgB))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var response struct {
			Data []model.DetailResponse `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.Greater(t, response.Meta.Count, 0)

		for _, d := range response.Data {
			assert.Equal(t, clusterB, d.ClusterUUID, "org B should only see cluster B")
			assert.Equal(t, "ns-b", d.Project, "org B should only see namespace ns-b")
		}
	})

	// Request as unknown org: should see nothing
	t.Run("unknown org sees nothing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift", nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader("orgNONEXISTENT"))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var response struct {
			Data []interface{} `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		assert.Equal(t, 0, response.Meta.Count)
	})
}

func TestGetNativeRecommendationSetList_FilterByCluster(t *testing.T) {
	// §17: API filter by cluster alias.
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	defer func() { database.DB = nil }()

	cluster1 := "c1111111-1111-1111-1111-111111111111"
	cluster2 := "c2222222-2222-2222-2222-222222222222"

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'alpha-cluster', 'src-1', now()), (1, $2, 'beta-cluster', 'src-2', now()) ON CONFLICT DO NOTHING`, cluster1, cluster2)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for i := 0; i < 7; i++ {
		for _, cl := range []struct{ uuid, ns string }{{cluster1, "ns-alpha"}, {cluster2, "ns-beta"}} {
			testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
				BucketDate: start.AddDate(0, 0, i),
				OrgID:      testutil.TestOrgID, ClusterUUID: cl.uuid,
				Namespace: cl.ns, Workload: "deploy", WorkloadType: "deployment",
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
	}

	end := start.AddDate(0, 0, 6)
	recs1, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, cluster1, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	recs2, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, cluster2, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, engine.WriteRecommendations(ctx, pool, recs1))
	require.NoError(t, engine.WriteRecommendations(ctx, pool, recs2))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?cluster=alpha-cluster", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data []model.DetailResponse `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Greater(t, response.Meta.Count, 0)

	for _, d := range response.Data {
		assert.Equal(t, "alpha-cluster", d.ClusterAlias,
			"filter by cluster should return only matching cluster")
	}
}

func TestGetNativeRecommendationSetList_FilterByNamespace(t *testing.T) {
	// §17: API filter by project (namespace).
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	defer func() { database.DB = nil }()

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for i := 0; i < 7; i++ {
		for _, ns := range []string{"production", "staging"} {
			testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
				BucketDate: start.AddDate(0, 0, i),
				OrgID:      testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
				Namespace: ns, Workload: "deploy-" + ns, WorkloadType: "deployment",
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
	}

	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, engine.WriteRecommendations(ctx, pool, recs))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?project=production", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data []model.DetailResponse `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Greater(t, response.Meta.Count, 0)

	for _, d := range response.Data {
		assert.Equal(t, "production", d.Project,
			"filter by project should return only matching namespace")
	}
}

func TestGetNativeRecommendationSetList_RBAC_FiltersByCluster(t *testing.T) {
	// RBAC test: When RBAC is enabled and user has permissions for only
	// one cluster, only that cluster's data should be returned.
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	defer func() { database.DB = nil }()

	cluster1 := "a1111111-1111-1111-1111-111111111111"
	cluster2 := "a2222222-2222-2222-2222-222222222222"

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'rbac-cluster-1', 'src-1', now()), (1, $2, 'rbac-cluster-2', 'src-2', now()) ON CONFLICT DO NOTHING`, cluster1, cluster2)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for i := 0; i < 7; i++ {
		for _, cl := range []struct{ uuid, ns string }{{cluster1, "ns-rbac-1"}, {cluster2, "ns-rbac-2"}} {
			testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
				BucketDate: start.AddDate(0, 0, i),
				OrgID:      testutil.TestOrgID, ClusterUUID: cl.uuid,
				Namespace: cl.ns, Workload: "deploy", WorkloadType: "deployment",
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
	}

	end := start.AddDate(0, 0, 6)
	recs1, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, cluster1, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	recs2, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, cluster2, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, engine.WriteRecommendations(ctx, pool, recs1))
	require.NoError(t, engine.WriteRecommendations(ctx, pool, recs2))

	// Enable RBAC temporarily
	cfg := config.GetConfig()
	origRBAC := cfg.RBACEnabled
	cfg.RBACEnabled = true
	defer func() { cfg.RBACEnabled = origRBAC }()

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	// Inject RBAC permissions: only allow cluster1
	v1.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user.permissions", map[string][]string{
				"openshift.cluster": {cluster1},
				"openshift.project": {"*"},
			})
			return next(c)
		}
	})
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data []model.DetailResponse `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Greater(t, response.Meta.Count, 0, "RBAC-filtered results should not be empty")

	for _, d := range response.Data {
		assert.Equal(t, cluster1, d.ClusterUUID,
			"RBAC should restrict results to the permitted cluster only")
	}
}

func TestGetNativeRecommendationSet_NotificationsInResponse(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	defer func() { database.DB = nil }()

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	// Seed data with OOM events so EvaluateNotifications produces codes.
	// Use recent dates (ending yesterday) to avoid the 3-day staleness filter.
	now := time.Now().UTC()
	recentStart := now.AddDate(0, 0, -7)
	for i := 0; i < 7; i++ {
		d := recentStart.AddDate(0, 0, i)
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:       d,
			OrgID:            testutil.TestOrgID,
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        testutil.TestNamespace,
			Workload:         testutil.TestWorkload,
			WorkloadType:     testutil.TestWorkloadType,
			ContainerName:    testutil.TestContainer,
			CPURequestP50MC:  200,
			CPURequestP95MC:  210,
			CPUUsageP50MC:    190,
			CPUUsageP95MC:    200,
			CPUUsageP98MC:    205,
			CPUUsageP99MC:    208,
			CPUUsageMaxMC:    215,
			CPUThrottleP95MC: 5,
			CPUThrottleMaxMC: 10,
			MemRequestP50KiB: 524288,
			MemRequestP95KiB: 524800,
			MemUsageP50KiB:   524000,
			MemUsageP95KiB:   524288,
			MemUsageMaxKiB:   525312,
			MemRSSP95KiB:     524000,
			MemRSSMaxKiB:     525000,
			OOMCountSum:      3,
			CPUUsageMeanMC:   195,
			MemUsageMeanKiB:  523000,
			SampleCount:      96,
		})
	}

	end := now.AddDate(0, 0, -1)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, recentStart, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	err = engine.WriteRecommendations(ctx, pool, recs)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/:recommendation-id", api.GetNativeRecommendationSet)

	containerID := model.NativeContainerID(testutil.TestClusterUUID, testutil.TestNamespace, testutil.TestWorkload, testutil.TestContainer)
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/"+containerID, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))

	recommendations, ok := raw["recommendations"].(map[string]interface{})
	require.True(t, ok, "response should have recommendations map")

	recTerms, ok := recommendations["recommendation_terms"].(map[string]interface{})
	require.True(t, ok, "recommendations should have recommendation_terms")

	notifFound := false
	for _, termData := range recTerms {
		termMap, ok := termData.(map[string]interface{})
		if !ok {
			continue
		}
		engines, ok := termMap["recommendation_engines"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, engineKey := range []string{"cost", "performance"} {
			engData, ok := engines[engineKey].(map[string]interface{})
			if !ok {
				continue
			}
			if notifs, ok := engData["notifications"]; ok && notifs != nil {
				notifMap, ok := notifs.(map[string]interface{})
				if ok && len(notifMap) > 0 {
					notifFound = true
					for _, v := range notifMap {
						entry, ok := v.(map[string]interface{})
						require.True(t, ok, "notification entry should be a map")
						assert.NotEmpty(t, entry["type"], "notification should have type")
						assert.NotEmpty(t, entry["message"], "notification should have message")
						assert.NotNil(t, entry["code"], "notification should have code")
					}
				}
			}
		}
	}
	assert.True(t, notifFound, "at least one engine recommendation should have notifications")
}

func TestGetNativeRecommendationSetList_GPUEnrichment(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	database.Pool = pool
	defer func() { database.DB = nil; database.Pool = nil }()

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'gpu-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()

	// Seed CPU/memory digests + recommendations
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	err = engine.WriteRecommendations(ctx, pool, recs)
	require.NoError(t, err)

	// Seed GPU digests for the same container — idle GPU with profiling data
	for i := 0; i < 7; i++ {
		testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
			IntervalStart:       start.AddDate(0, 0, i),
			ClusterUUID:         testutil.TestClusterUUID,
			Namespace:           testutil.TestNamespace,
			Workload:            testutil.TestWorkload,
			WorkloadType:        testutil.TestWorkloadType,
			ContainerName:       testutil.TestContainer,
			GPUModelName:        "NVIDIA A100-SXM4-80GB",
			GPUProfileName:      "",
			FBUsageMinMiB:       100,
			FBUsageMaxMiB:       500,
			FBUsageAvgMiB:       250,
			TensorPipeActiveMin: 0.001,
			TensorPipeActiveMax: 0.01,
			TensorPipeActiveAvg: 0.005,
			DRAMActiveMin:       0.001,
			DRAMActiveMax:       0.01,
			DRAMActiveAvg:       0.005,
			SMActiveMin:         0.001,
			SMActiveMax:         0.01,
			SMActiveAvg:         0.005,
		})
	}

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	t.Run("response includes GPU block", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		var response struct {
			Data []model.DetailResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.NotEmpty(t, response.Data)

		found := false
		for _, d := range response.Data {
			if len(d.GPU) > 0 {
				found = true
				for _, gpuRec := range d.GPU {
					assert.Equal(t, "NVIDIA A100-SXM4-80GB", gpuRec.CurrentGPUModel)
					assert.Equal(t, "idle", gpuRec.GPUClassification)
					assert.Greater(t, gpuRec.GPUConfidence, float32(0))
					break
				}
				break
			}
		}
		assert.True(t, found, "at least one result should have GPU data")
	})

	t.Run("has_gpu=true filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?has_gpu=true", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var response struct {
			Data []model.DetailResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		for _, d := range response.Data {
			assert.NotEmpty(t, d.GPU, "has_gpu=true should only return items with GPU data")
		}
	})

	t.Run("has_gpu=false filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?has_gpu=false", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var response struct {
			Data []model.DetailResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		for _, d := range response.Data {
			assert.Empty(t, d.GPU, "has_gpu=false should only return items without GPU data")
		}
	})

	t.Run("gpu_model filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?gpu_model=A100", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var response struct {
			Data []model.DetailResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.NotEmpty(t, response.Data)
		for _, d := range response.Data {
			require.NotEmpty(t, d.GPU)
			for _, gpuRec := range d.GPU {
				assert.Contains(t, gpuRec.CurrentGPUModel, "A100")
				break
			}
		}
	})

	t.Run("gpu_classification filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?gpu_classification=idle", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var response struct {
			Data []model.DetailResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.NotEmpty(t, response.Data)
		for _, d := range response.Data {
			require.NotEmpty(t, d.GPU)
			for _, gpuRec := range d.GPU {
				assert.Equal(t, "idle", gpuRec.GPUClassification)
				break
			}
		}
	})
}

func TestGetNativeRecommendationSetList_EmptyResults(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data []interface{} `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, 0, response.Meta.Count)

	database.DB = nil
}
