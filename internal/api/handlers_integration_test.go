package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

	// Seed digests and compute recommendations
	testutil.SeedDigestSeries(t, pool, 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
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
			Data []model.NativeContainerResult `json:"data"`
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
		assert.NotEmpty(t, first.Recommendations)
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
