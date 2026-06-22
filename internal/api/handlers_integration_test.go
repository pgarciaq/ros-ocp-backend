package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	"entitlements": map[string]interface{}{
			"cost_management": map[string]interface{}{
				"is_entitled": true,
			},
		},
	}
	b, _ := json.Marshal(id)
	return base64.StdEncoding.EncodeToString(b)
}

func TestGetNativeRecommendationSetList_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	// Seed rh_accounts and clusters so the JOIN resolves
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
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

	err = engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs)
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
}

func TestGetNativeRecommendationSetList_PaginationCount(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
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
	err = engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs)
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

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
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
	err = engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs)
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
		containerID := model.NativeContainerID(testutil.TestClusterUUID, testutil.TestNamespace, testutil.TestWorkload, testutil.TestWorkloadType, testutil.TestContainer)
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/"+containerID, nil)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestGetNativeRecommendationSet_DetailExplanationOptIn(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
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
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)
	v1.GET("/recommendations/openshift/:recommendation-id", api.GetNativeRecommendationSet)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	listReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?limit=1", nil)
	listReq.Header.Set("X-Rh-Identity", identityHeader)
	listRec := httptest.NewRecorder()
	app.ServeHTTP(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code)

	var listResp struct {
		Data []model.DetailResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &listResp))
	require.NotEmpty(t, listResp.Data)
	containerID := listResp.Data[0].ID

	t.Run("default detail omits explanation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/"+containerID, nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
		recsBlock, ok := raw["recommendations"].(map[string]any)
		require.True(t, ok)
		terms, ok := recsBlock["recommendation_terms"].(map[string]any)
		require.True(t, ok)
		for _, termVal := range terms {
			termMap, ok := termVal.(map[string]any)
			require.True(t, ok)
			engines, ok := termMap["recommendation_engines"].(map[string]any)
			require.True(t, ok)
			for _, engVal := range engines {
				engMap, ok := engVal.(map[string]any)
				require.True(t, ok)
				assert.NotContains(t, engMap, "explanation")
			}
		}
	})

	t.Run("include=explanation adds explanation when populated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/"+containerID+"?include=explanation", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var detail model.DetailResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
		require.NotEmpty(t, detail.Recommendations.RecommendationTerms)

		found := false
		for _, term := range detail.Recommendations.RecommendationTerms {
			if term.RecommendationEngines == nil {
				continue
			}
			for _, eng := range []*model.DetailEngine{term.RecommendationEngines.Cost, term.RecommendationEngines.Performance} {
				if eng == nil {
					continue
				}
				if eng.Explanation != nil && eng.Explanation.DataDays != nil {
					found = true
					assert.Greater(t, *eng.Explanation.DataDays, 0)
				}
			}
		}
		assert.True(t, found, "expected at least one engine with populated explanation.data_days")
	})
}

func TestGetContainerDetail_DualEnginePresence(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)
	v1.GET("/recommendations/openshift/:recommendation-id", api.GetNativeRecommendationSet)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	listReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?limit=1", nil)
	listReq.Header.Set("X-Rh-Identity", identityHeader)
	listRec := httptest.NewRecorder()
	app.ServeHTTP(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code)

	var listResp struct {
		Data []model.DetailResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &listResp))
	require.NotEmpty(t, listResp.Data)

	containerID := listResp.Data[0].ID
	detailReq := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/"+containerID, nil)
	detailReq.Header.Set("X-Rh-Identity", identityHeader)
	detailRec := httptest.NewRecorder()
	app.ServeHTTP(detailRec, detailReq)
	require.Equal(t, http.StatusOK, detailRec.Code)

	var detail map[string]interface{}
	require.NoError(t, json.Unmarshal(detailRec.Body.Bytes(), &detail))
	terms := detail["recommendations"].(map[string]interface{})["recommendation_terms"].(map[string]interface{})

	foundCost := false
	foundPerf := false
	for _, termRaw := range terms {
		term, ok := termRaw.(map[string]interface{})
		if !ok {
			continue
		}
		engines, ok := term["recommendation_engines"].(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := engines["cost"].(map[string]interface{}); ok {
			foundCost = true
		}
		if _, ok := engines["performance"].(map[string]interface{}); ok {
			foundPerf = true
		}
	}
	assert.True(t, foundCost, "detail should include recommendation_engines.cost")
	assert.True(t, foundPerf, "detail should include recommendation_engines.performance")
}

// TestContainerDetail_EngineValuesDiverge requires both engines on detail; for divergent
// sizing in manual/E2E runs, ingest nise/examples/ocp_dual_engine/.
func TestContainerDetail_EngineValuesDiverge(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)
	v1.GET("/recommendations/openshift/:recommendation-id", api.GetNativeRecommendationSet)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	listReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?limit=20", nil)
	listReq.Header.Set("X-Rh-Identity", identityHeader)
	listRec := httptest.NewRecorder()
	app.ServeHTTP(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code)

	var listResp struct {
		Data []model.DetailResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &listResp))
	require.NotEmpty(t, listResp.Data)

	engineCPUAmount := func(engine map[string]interface{}) (float64, bool) {
		config, ok := engine["config"].(map[string]interface{})
		if !ok {
			return 0, false
		}
		requests, ok := config["requests"].(map[string]interface{})
		if !ok {
			return 0, false
		}
		cpu, ok := requests["cpu"].(map[string]interface{})
		if !ok {
			return 0, false
		}
		amount, ok := cpu["amount"].(float64)
		return amount, ok
	}

	diverged := false
	for _, item := range listResp.Data {
		detailReq := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/"+item.ID, nil)
		detailReq.Header.Set("X-Rh-Identity", identityHeader)
		detailRec := httptest.NewRecorder()
		app.ServeHTTP(detailRec, detailReq)
		require.Equal(t, http.StatusOK, detailRec.Code, detailRec.Body.String())

		var detail map[string]interface{}
		require.NoError(t, json.Unmarshal(detailRec.Body.Bytes(), &detail))
		terms := detail["recommendations"].(map[string]interface{})["recommendation_terms"].(map[string]interface{})

		foundCost := false
		foundPerf := false
		for _, termRaw := range terms {
			term, ok := termRaw.(map[string]interface{})
			if !ok {
				continue
			}
			engines, ok := term["recommendation_engines"].(map[string]interface{})
			if !ok {
				continue
			}
			costEng, costOK := engines["cost"].(map[string]interface{})
			perfEng, perfOK := engines["performance"].(map[string]interface{})
			if costOK {
				foundCost = true
			}
			if perfOK {
				foundPerf = true
			}
			if !costOK || !perfOK {
				continue
			}
			costCPU, costHas := engineCPUAmount(costEng)
			perfCPU, perfHas := engineCPUAmount(perfEng)
			if costHas && perfHas && costCPU != perfCPU {
				diverged = true
			}
		}
		assert.True(t, foundCost, "detail %s should include recommendation_engines.cost", item.ID)
		assert.True(t, foundPerf, "detail %s should include recommendation_engines.performance", item.ID)
	}

	if !diverged {
		t.Log("cost and performance values are equal for this test data; divergence not verifiable (use nise/examples/ocp_dual_engine for divergent fixtures)")
	}
}

func TestGetNamespaceList_FilterEngine(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'ns-engine-cluster', 'src-ns-eng', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	testutil.SeedNamespaceDigestSeries(t, pool, "ns-engine-filter", 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := engine.RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.NoError(t, engine.WriteNamespaceRecommendations(ctx, pool, results))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/namespaces", api.GetNamespaceRecommendationSetListWithFallback)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)
	basePath := "/api/cost-management/v1/recommendations/openshift/namespaces"

	for _, tc := range []struct {
		name       string
		query      string
		wantEngine string
	}{
		{"filter engine cost", "?filter%5Bengine%5D=cost&limit=10", "cost"},
		{"filter engine performance", "?filter%5Bengine%5D=performance&limit=10", "performance"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, basePath+tc.query, nil)
			req.Header.Set("X-Rh-Identity", identityHeader)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assertContainerListEngineFilterResponse(t, rec.Body.Bytes(), tc.wantEngine)
		})
	}
}

func TestGetNativeRecommendationSetList_OrgIsolation(t *testing.T) {
	// T-2.2: Org A must not see org B's recommendations.
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	orgA := "orgAAAAAAAA"
	orgB := "orgBBBBBBBB"
	clusterA := "aaaa1111-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	clusterB := "bbbb2222-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	// Seed rh_accounts and clusters for both orgs
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (100, $1), (200, $2) ON CONFLICT DO NOTHING`, orgA, orgB)
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
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recsA))
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recsB))

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

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	cluster1 := "c1111111-1111-1111-1111-111111111111"
	cluster2 := "c2222222-2222-2222-2222-222222222222"

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
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
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs1))
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs2))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?filter%5Bcluster%5D=alpha-cluster", nil)
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

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
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
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?filter%5Bproject%5D=production", nil)
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

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	cluster1 := "a1111111-1111-1111-1111-111111111111"
	cluster2 := "a2222222-2222-2222-2222-222222222222"

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
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
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs1))
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs2))

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
	assert.Equal(t, len(response.Data), response.Meta.Count,
		"meta.count must reflect RBAC-filtered total, not org-wide count")

	for _, d := range response.Data {
		assert.Equal(t, cluster1, d.ClusterUUID,
			"RBAC should restrict results to the permitted cluster only")
	}
}

func TestGetNativeRecommendationSetList_RBAC_MetaCountZeroWhenNoAccessibleClusters(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	clusterWithData := "a1111111-1111-1111-1111-111111111111"
	clusterDenied := "b2222222-2222-2222-2222-222222222222"

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'rbac-data-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, clusterWithData)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate: start.AddDate(0, 0, i),
			OrgID:      testutil.TestOrgID, ClusterUUID: clusterWithData,
			Namespace: "ns-rbac-deny", Workload: "deploy", WorkloadType: "deployment",
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
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, clusterWithData, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	cfg := config.GetConfig()
	origRBAC := cfg.RBACEnabled
	cfg.RBACEnabled = true
	defer func() { cfg.RBACEnabled = origRBAC }()

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user.permissions", map[string][]string{
				"openshift.cluster": {clusterDenied},
				"openshift.project": {"*"},
			})
			return next(c)
		}
	})
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
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Empty(t, response.Data)
	assert.Equal(t, 0, response.Meta.Count,
		"meta.count must be zero when RBAC denies all clusters with data")
}

func TestGetNativeRecommendationSet_NotificationsInResponse(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
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
	err = engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/:recommendation-id", api.GetNativeRecommendationSet)

	containerID := model.NativeContainerID(testutil.TestClusterUUID, testutil.TestNamespace, testutil.TestWorkload, testutil.TestWorkloadType, testutil.TestContainer)
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

	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
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
	err = engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs)
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

	// Mark containers with GPU data and compute classifications for SQL-level filtering
	err = engine.MarkContainersWithGPU(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)
	err = engine.StoreGPUClassifications(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, engine.DefaultTerms(), nil)
	require.NoError(t, err)

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
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?filter%5Bhas_gpu%5D=true", nil)
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
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?filter%5Bhas_gpu%5D=false", nil)
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
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?filter%5Bgpu_model%5D=A100", nil)
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

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
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
}

func TestGetNativeRecommendationSetList_PaginationLinks(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	orgID := "orgPAGINATION"
	clusterUUID := "cccc3333-cccc-cccc-cccc-cccccccccccc"

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (300, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (300, $1, 'pagination-cluster', 'src-pag', now()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	containers := []string{"container-a", "container-b", "container-c", "container-d", "container-e"}
	for _, cname := range containers {
		for i := 0; i < 7; i++ {
			testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
				BucketDate: start.AddDate(0, 0, i),
				OrgID:      orgID, ClusterUUID: clusterUUID,
				Namespace: "ns-pag", Workload: "deploy-pag", WorkloadType: "deployment",
				ContainerName:   cname,
				CPURequestP50MC: 100, CPURequestP95MC: 120,
				CPUUsageP50MC: 90, CPUUsageP95MC: 110, CPUUsageP98MC: 115,
				CPUUsageP99MC: 118, CPUUsageMaxMC: 125,
				CPUThrottleP95MC: 5, CPUThrottleMaxMC: 10,
				MemRequestP50KiB: 524288, MemRequestP60KiB: 524500, MemRequestP95KiB: 524800,
				MemRequestP98KiB: 525000, MemRequestP99KiB: 525100,
				MemUsageP50KiB: 524000, MemUsageP60KiB: 524100, MemUsageP95KiB: 524288,
				MemUsageP98KiB: 524500, MemUsageP99KiB: 524700,
				MemUsageMaxKiB: 525312, MemRSSP95KiB: 524000, MemRSSMaxKiB: 525000,
				OOMCountSum: 0, CPUUsageMeanMC: 95, MemUsageMeanKiB: 523000,
				SampleCount: 96,
			})
		}
	}

	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, orgID, clusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	doRequest := func(limit, offset int) *httptest.ResponseRecorder {
		url := fmt.Sprintf("/api/cost-management/v1/recommendations/openshift?limit=%d&offset=%d", limit, offset)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		return rec
	}

	type linksResponse struct {
		Data []interface{} `json:"data"`
		Meta struct {
			Count  int `json:"count"`
			Limit  int `json:"limit"`
			Offset int `json:"offset"`
		} `json:"meta"`
		Links struct {
			First    string `json:"first"`
			Previous string `json:"previous"`
			Next     string `json:"next"`
			Last     string `json:"last"`
		} `json:"links"`
	}

	t.Run("first page has no previous, has next", func(t *testing.T) {
		rec := doRequest(2, 0)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp linksResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, 5, resp.Meta.Count)
		assert.Len(t, resp.Data, 2)

		assert.Contains(t, resp.Links.First, "offset=0")
		assert.Contains(t, resp.Links.First, "limit=2")
		assert.Empty(t, resp.Links.Previous, "first page should have no previous link")
		assert.Contains(t, resp.Links.Next, "after=")
		assert.Contains(t, resp.Links.Last, "offset=4")
	})

	t.Run("middle page has both previous and next", func(t *testing.T) {
		rec := doRequest(2, 2)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp linksResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, 5, resp.Meta.Count)
		assert.Len(t, resp.Data, 2)

		assert.Contains(t, resp.Links.First, "offset=0")
		assert.Contains(t, resp.Links.Previous, "offset=0")
		assert.Contains(t, resp.Links.Next, "after=")
		assert.Contains(t, resp.Links.Last, "offset=4")
	})

	t.Run("last page has previous, no next", func(t *testing.T) {
		rec := doRequest(2, 4)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp linksResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, 5, resp.Meta.Count)
		assert.Len(t, resp.Data, 1, "last page should have 1 remaining item")

		assert.Contains(t, resp.Links.First, "offset=0")
		assert.Contains(t, resp.Links.Previous, "offset=2")
		assert.Empty(t, resp.Links.Next, "last page should have no next link")
		assert.Contains(t, resp.Links.Last, "offset=4")
	})

	t.Run("single page (limit >= count) has no previous or next", func(t *testing.T) {
		rec := doRequest(100, 0)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp linksResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, 5, resp.Meta.Count)
		assert.Len(t, resp.Data, 5)

		assert.Contains(t, resp.Links.First, "offset=0")
		assert.Empty(t, resp.Links.Previous)
		assert.Empty(t, resp.Links.Next)
		assert.Contains(t, resp.Links.Last, "offset=0")
	})
}

func TestGetNativeRecommendationSetList_BracketFilterSyntax(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate: start.AddDate(0, 0, i),
			OrgID:      testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
			Namespace: "payments", Workload: "api-deploy", WorkloadType: "deployment",
			ContainerName:   "web",
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
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)
	basePath := "/api/cost-management/v1/recommendations/openshift"

	tests := []struct {
		name  string
		query string
	}{
		{"filter namespace alias", "?filter%5Bnamespace%5D=payments&limit=10"},
		{"filter container", "?filter%5Bcontainer%5D=web&limit=10"},
		{"filter workload", "?filter%5Bworkload%5D=api-deploy&limit=10"},
		{"filter workload_type deployment", "?filter%5Bworkload_type%5D=deployment&limit=10"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, basePath+tc.query, nil)
			req.Header.Set("X-Rh-Identity", identityHeader)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		})
	}

	for _, tc := range []struct {
		name       string
		query      string
		wantEngine string
	}{
		{"filter engine cost", "?filter%5Bengine%5D=cost&limit=10", "cost"},
		{"filter engine performance", "?filter%5Bengine%5D=performance&limit=10", "performance"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, basePath+tc.query, nil)
			req.Header.Set("X-Rh-Identity", identityHeader)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assertContainerListEngineFilterResponse(t, rec.Body.Bytes(), tc.wantEngine)
		})
	}
}

func TestGetContainerList_FilterTerm(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'term-filter-cluster', 'src-tf', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate: start.AddDate(0, 0, i),
			OrgID:      testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
			Namespace: "payments", Workload: "api-deploy", WorkloadType: "deployment",
			ContainerName:   "web",
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
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?filter%5Bterm%5D=short_term&limit=10", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assertContainerListTermFilterResponse(t, rec.Body.Bytes(), "short_term")
}

func assertContainerListTermFilterResponse(t *testing.T, body []byte, wantTerm string) {
	t.Helper()

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &resp))
	require.Contains(t, resp, "data")

	data, ok := resp["data"].([]interface{})
	require.True(t, ok, "data must be an array")
	require.NotEmpty(t, data, "expected non-empty data")

	for _, rawItem := range data {
		item, ok := rawItem.(map[string]interface{})
		require.True(t, ok)
		recs, ok := item["recommendations"].(map[string]interface{})
		require.True(t, ok, "list item missing recommendations")
		terms, ok := recs["recommendation_terms"].(map[string]interface{})
		require.True(t, ok, "recommendations missing recommendation_terms")

		require.Contains(t, terms, wantTerm, "filter[term]=%s should include %q", wantTerm, wantTerm)
		for termKey := range terms {
			assert.Equal(t, wantTerm, termKey,
				"filter[term]=%s should omit other recommendation terms", wantTerm)
		}
	}
}

func assertContainerListEngineFilterResponse(t *testing.T, body []byte, wantEngine string) {
	t.Helper()

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &resp))
	require.Contains(t, resp, "meta")
	require.Contains(t, resp, "data")

	data, ok := resp["data"].([]interface{})
	require.True(t, ok, "data must be an array")
	require.NotEmpty(t, data, "expected non-empty data")

	otherEngine := "performance"
	if wantEngine == "performance" {
		otherEngine = "cost"
	}

	filterOmitsOtherEngine := true
	for _, rawItem := range data {
		item, ok := rawItem.(map[string]interface{})
		require.True(t, ok)
		recs, ok := item["recommendations"].(map[string]interface{})
		require.True(t, ok, "list item missing recommendations")
		terms, ok := recs["recommendation_terms"].(map[string]interface{})
		require.True(t, ok, "recommendations missing recommendation_terms")

		foundWant := false
		for _, termRaw := range terms {
			term, ok := termRaw.(map[string]interface{})
			if !ok {
				continue
			}
			engines, ok := term["recommendation_engines"].(map[string]interface{})
			if !ok {
				continue
			}
			if _, ok := engines[wantEngine].(map[string]interface{}); ok {
				foundWant = true
			}
			if _, ok := engines[otherEngine]; ok {
				filterOmitsOtherEngine = false
			}
		}
		assert.True(t, foundWant, "expected %q engine data under recommendation_engines", wantEngine)
	}

	if filterOmitsOtherEngine {
		for _, rawItem := range data {
			item := rawItem.(map[string]interface{})
			terms := item["recommendations"].(map[string]interface{})["recommendation_terms"].(map[string]interface{})
			for _, termRaw := range terms {
				term := termRaw.(map[string]interface{})
				engines, ok := term["recommendation_engines"].(map[string]interface{})
				if !ok {
					continue
				}
				assert.NotContains(t, engines, otherEngine,
					"filter[engine]=%s should omit %q from recommendation_engines", wantEngine, otherEngine)
			}
		}
	}
}

func TestGetNativeRecommendationSetList_OrderByVariationFields(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate: start.AddDate(0, 0, i),
			OrgID:      testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
			Namespace: "payments", Workload: "api-deploy", WorkloadType: "deployment",
			ContainerName:   "web",
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
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)
	basePath := "/api/cost-management/v1/recommendations/openshift"

	orderByFields := []string{
		"cpu_variation_medium_cost",
		"memory_variation_long_performance",
		"cpu_variation_short_cost",
		"cpu_variation_short_performance",
		"cpu_variation_medium_performance",
		"cpu_variation_long_cost",
		"cpu_variation_long_performance",
		"memory_variation_short_cost",
		"memory_variation_short_performance",
		"memory_variation_medium_cost",
		"memory_variation_medium_performance",
		"memory_variation_long_cost",
	}

	for _, orderBy := range orderByFields {
		t.Run(orderBy, func(t *testing.T) {
			query := "?order_by=" + orderBy + "&order_how=desc&limit=10"
			req := httptest.NewRequest(http.MethodGet, basePath+query, nil)
			req.Header.Set("X-Rh-Identity", identityHeader)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		})
	}
}

func TestGetNativeRecommendationSetList_OrderByVariationAlias(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate: start.AddDate(0, 0, i),
			OrgID:      testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
			Namespace: "payments", Workload: "api-deploy", WorkloadType: "deployment",
			ContainerName:   "web",
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
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)
	query := "?order_by=cpu_variation&filter[term]=short_term&filter[engine]=cost&limit=10"
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift"+query, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestGetNativeRecommendationSetList_OrderByNonVariationFields(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate: start.AddDate(0, 0, i),
			OrgID:      testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
			Namespace: "payments", Workload: "api-deploy", WorkloadType: "deployment",
			ContainerName:   "web",
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
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)
	basePath := "/api/cost-management/v1/recommendations/openshift"

	orderByFields := []string{
		"last_reported",
		"cluster",
		"project",
		"workload",
		"workload_type",
		"container",
		"cpu_request_current",
		"memory_request_current",
	}

	for _, orderBy := range orderByFields {
		t.Run(orderBy, func(t *testing.T) {
			query := "?order_by=" + orderBy + "&order_how=desc&limit=10"
			req := httptest.NewRequest(http.MethodGet, basePath+query, nil)
			req.Header.Set("X-Rh-Identity", identityHeader)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		})
	}
}

func TestGetNativeRecommendationSetList_OrderByEstimatedMonthlySavings(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'savings-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			term, engine, stale, notification_codes, estimated_savings_cents, updated_at)
		VALUES
			($1, $2, 'ns1', 'deploy-a', 'Deployment', 'app-low',  'medium', 'cost', false, '{}', 10000, now()),
			($1, $2, 'ns1', 'deploy-b', 'Deployment', 'app-high', 'medium', 'cost', false, '{}', 90000, now()),
			($1, $2, 'ns1', 'deploy-c', 'Deployment', 'app-mid',  'medium', 'cost', false, '{}', 50000, now())
		ON CONFLICT DO NOTHING`, testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, testutil.TestOrgID))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)
	basePath := "/api/cost-management/v1/recommendations/openshift"

	type listBody struct {
		Data []struct {
			Container               string `json:"container"`
			EstimatedMonthlySavings *struct {
				Value string `json:"value"`
			} `json:"estimated_monthly_savings"`
		} `json:"data"`
	}

	t.Run("descending", func(t *testing.T) {
		query := "?order_by=estimated_monthly_savings&order_how=desc&limit=10"
		req := httptest.NewRequest(http.MethodGet, basePath+query, nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var body listBody
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.GreaterOrEqual(t, len(body.Data), 3)
		assert.Equal(t, "app-high", body.Data[0].Container)
		assert.Equal(t, "app-mid", body.Data[1].Container)
		assert.Equal(t, "app-low", body.Data[2].Container)
	})

	t.Run("ascending", func(t *testing.T) {
		query := "?order_by=estimated_monthly_savings&order_how=asc&limit=10"
		req := httptest.NewRequest(http.MethodGet, basePath+query, nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var body listBody
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.GreaterOrEqual(t, len(body.Data), 3)
		assert.Equal(t, "app-low", body.Data[0].Container)
		assert.Equal(t, "app-mid", body.Data[1].Container)
		assert.Equal(t, "app-high", body.Data[2].Container)
	})
}
