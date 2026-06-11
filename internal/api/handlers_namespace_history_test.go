package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestNamespaceRecommendationHistory_APIEndpoint(t *testing.T) {
	orgID := testutil.TestOrgID
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'ns-hist-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	testutil.SeedNamespaceDigestSeries(t, pool, "ns-api-hist", 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := engine.RecommendAllNamespaces(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.NoError(t, engine.WriteNamespaceRecommendations(ctx, pool, results))
	require.NoError(t, engine.WriteNamespaceRecommendationHistory(ctx, pool, results))

	namespaceID := model.NativeNamespaceID(testutil.TestClusterUUID, "ns-api-hist")

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/namespaces/:recommendation-id/history", api.GetNamespaceRecommendationHistory)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/namespaces/"+namespaceID+"/history?filter[term]=short_term&filter[engine]=cost&limit=10",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var resp struct {
		Data []engine.NamespaceRecommendationHistoryRow `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.Greater(t, resp.Meta.Count, 0)
	assert.NotEmpty(t, resp.Data)
	assert.Equal(t, "cpu", resp.Data[0].Resource)
}
