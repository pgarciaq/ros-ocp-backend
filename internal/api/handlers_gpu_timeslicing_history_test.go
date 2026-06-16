package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestNodeGPUTimeslicingHistory_APIEndpoint(t *testing.T) {
	orgID := "org-gpu-ts-hist-api-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedVMRecCluster(t, orgID)
	clusterUUID := testutil.TestClusterUUID
	nodeName := "api-gpu-node"

	_, err := pool.Exec(context.Background(), `
		INSERT INTO node_gpu_timeslicing_recommendation_history (
			org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, candidate_count, impacted_count
		) VALUES ($1, $2::uuid, $3, 'L4', 'medium', 4, 0.8, 2, 1)`,
		orgID, clusterUUID, nodeName,
	)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/gpu/timeslicing/history", api.GetNodeGPUTimeslicingRecommendationHistory)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing/history?cluster_uuid="+clusterUUID+"&node_name="+nodeName,
		nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.NodeGPUTimeslicingHistoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, nodeName, resp.Data[0].NodeName)
	assert.Equal(t, int32(4), resp.Data[0].RecommendedReplicas)
}

func TestNodeGPUTimeslicingHistory_APIRequiresParams(t *testing.T) {
	orgID := testutil.TestOrgID
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/gpu/timeslicing/history", api.GetNodeGPUTimeslicingRecommendationHistory)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing/history?node_name=node1",
		nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
