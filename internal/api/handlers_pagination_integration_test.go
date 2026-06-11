package api_test

import (
	"context"
	"encoding/json"
	"fmt"
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

func seedNativeRecommendationsForPagination(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
		BucketDate:       start,
		OrgID:            testutil.TestOrgID,
		ClusterUUID:      testutil.TestClusterUUID,
		Namespace:        testutil.TestNamespace,
		Workload:         "workload-b",
		WorkloadType:     testutil.TestWorkloadType,
		ContainerName:    "container-b",
		CPURequestP50MC:  180,
		CPURequestP95MC:  210,
		CPUUsageP50MC:    170,
		CPUUsageP95MC:    200,
		MemRequestP50KiB: 512000,
		MemRequestP95KiB: 524288,
		MemUsageP50KiB:   500000,
		MemUsageP95KiB:   520000,
	})
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	require.NoError(t, engine.WriteRecommendations(ctx, pool, recs))
}

func TestGetNativeRecommendationSetList_CursorPagination(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	seedNativeRecommendationsForPagination(t, pool)
	t.Cleanup(func() { database.DB = nil })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	firstReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?limit=1", nil)
	firstReq.Header.Set("X-Rh-Identity", identityHeader)
	firstRec := httptest.NewRecorder()
	app.ServeHTTP(firstRec, firstReq)
	require.Equal(t, http.StatusOK, firstRec.Code, firstRec.Body.String())

	var firstPage struct {
		Data []model.DetailResponse `json:"data"`
		Meta struct {
			Count      int    `json:"count"`
			Limit      int    `json:"limit"`
			HasNext    bool   `json:"has_next"`
			NextCursor string `json:"next_cursor"`
			Offset     int    `json:"offset"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(firstRec.Body.Bytes(), &firstPage))
	require.Len(t, firstPage.Data, 1)
	assert.Equal(t, 1, firstPage.Meta.Limit)
	assert.Equal(t, 0, firstPage.Meta.Offset)
	assert.GreaterOrEqual(t, firstPage.Meta.Count, 1)

	if firstPage.Meta.Count <= 1 {
		assert.False(t, firstPage.Meta.HasNext)
		assert.Empty(t, firstPage.Meta.NextCursor)
		return
	}

	assert.True(t, firstPage.Meta.HasNext)
	require.NotEmpty(t, firstPage.Meta.NextCursor)

	secondReq := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/cost-management/v1/recommendations/openshift?limit=1&after=%s", firstPage.Meta.NextCursor),
		nil)
	secondReq.Header.Set("X-Rh-Identity", identityHeader)
	secondRec := httptest.NewRecorder()
	app.ServeHTTP(secondRec, secondReq)
	require.Equal(t, http.StatusOK, secondRec.Code, secondRec.Body.String())

	var secondPage struct {
		Data []model.DetailResponse `json:"data"`
		Meta struct {
			HasNext bool `json:"has_next"`
			Offset  int  `json:"offset"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(secondRec.Body.Bytes(), &secondPage))
	require.Len(t, secondPage.Data, 1)
	assert.Equal(t, 0, secondPage.Meta.Offset)
	assert.NotEqual(t, firstPage.Data[0].ID, secondPage.Data[0].ID)
}

func TestGetNativeRecommendationSetList_OffsetBackwardCompat(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	seedNativeRecommendationsForPagination(t, pool)
	t.Cleanup(func() { database.DB = nil })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?limit=5&offset=0", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Meta struct {
			Count   int  `json:"count"`
			Offset  int  `json:"offset"`
			Limit   int  `json:"limit"`
			HasNext bool `json:"has_next"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Equal(t, 0, response.Meta.Offset)
	assert.Equal(t, 5, response.Meta.Limit)
	assert.Greater(t, response.Meta.Count, 0)
}

func TestGetNativeRecommendationSetList_InvalidAfter(t *testing.T) {
	testutil.SetupTestDB(t)
	t.Cleanup(func() { database.DB = nil })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?after=not-valid", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
