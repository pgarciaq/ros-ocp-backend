package api_test

import (
	"context"
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
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupHistoryTest(t *testing.T) (*echo.Echo, string) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	t.Cleanup(func() { database.DB = nil })

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'history-test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)

	err = engine.WriteRecommendations(ctx, pool, recs)
	require.NoError(t, err)

	engine.EnsureHistoryPartitions(ctx, pool)
	err = engine.WriteRecommendationHistory(ctx, pool, recs, "")
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/history", api.GetRecommendationHistory)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)
	return app, identityHeader
}

func TestGetRecommendationHistory_Integration(t *testing.T) {
	app, identityHeader := setupHistoryTest(t)

	t.Run("JSON response", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/history", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		var response struct {
			Data []model.HistoryRow `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.Greater(t, response.Meta.Count, 0, "should have results")
		require.NotEmpty(t, response.Data)

		first := response.Data[0]
		assert.Equal(t, testutil.TestClusterUUID, first.ClusterUUID)
		assert.Equal(t, "history-test-cluster", first.ClusterAlias)
		assert.Equal(t, testutil.TestNamespace, first.Namespace)
		assert.NotEmpty(t, first.Term)
		assert.NotEmpty(t, first.Engine)
	})

	t.Run("CSV response", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/history?format=csv", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/csv")
		body := rec.Body.String()
		assert.Contains(t, body, "recorded_at")
		assert.Contains(t, body, testutil.TestClusterUUID)
	})

	t.Run("filter by term", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/history?term=short", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response struct {
			Data []model.HistoryRow  `json:"data"`
			Meta struct{ Count int } `json:"meta"`
		}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		for _, row := range response.Data {
			assert.Equal(t, "short", row.Term)
		}
	})

	t.Run("filter by engine cost", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/history?filter%5Bengine%5D=cost&limit=100", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var response struct {
			Data []model.HistoryRow `json:"data"`
		}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.NotEmpty(t, response.Data)
		for _, row := range response.Data {
			assert.Equal(t, "cost", row.Engine)
		}
	})

	t.Run("filter by engine performance", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/history?filter%5Bengine%5D=performance&limit=100", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var response struct {
			Data []model.HistoryRow `json:"data"`
		}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.NotEmpty(t, response.Data)
		for _, row := range response.Data {
			assert.Equal(t, "performance", row.Engine)
		}
	})

	t.Run("filter by container", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/history?filter%5Bcontainer%5D="+testutil.TestContainer, nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response struct {
			Data []model.HistoryRow  `json:"data"`
			Meta struct{ Count int } `json:"meta"`
		}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		for _, row := range response.Data {
			assert.Equal(t, testutil.TestContainer, row.ContainerName)
		}
	})

	t.Run("date range filter excludes data", func(t *testing.T) {
		farFuture := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/history?start_date="+farFuture, nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response struct {
			Data []model.HistoryRow  `json:"data"`
			Meta struct{ Count int } `json:"meta"`
		}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, 0, response.Meta.Count)
	})

	t.Run("pagination limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/history?limit=2", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response struct {
			Data []model.HistoryRow `json:"data"`
			Meta struct {
				Count  int `json:"count"`
				Limit  int `json:"limit"`
				Offset int `json:"offset"`
			} `json:"meta"`
		}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(response.Data), 2)
		assert.Equal(t, 2, response.Meta.Limit)
	})

	t.Run("missing identity returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/history", nil)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("wrong org returns empty", func(t *testing.T) {
		wrongOrgHeader := makeIdentityHeader("org9999999")
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/history", nil)
		req.Header.Set("X-Rh-Identity", wrongOrgHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response struct {
			Meta struct{ Count int } `json:"meta"`
		}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, 0, response.Meta.Count)
	})
}
