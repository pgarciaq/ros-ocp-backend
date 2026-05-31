package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
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

func TestVMRecHistory_APIEndpoint(t *testing.T) {
	orgID := "org-vm-hist-api-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedVMRecCluster(t, orgID)
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	now := time.Now().UTC()

	rec := model.VMRecommendation{
		OrgID: orgID, ClusterUUID: clusterID,
		VMName: "api-hist-vm", Namespace: "api-ns",
		Term: "short_term", Engine: "performance",
		RecommendedVCPU: 2, RecommendedMemoryGiB: 8,
		Confidence: "high", LastRecommendedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, engine.PersistVMRecommendations(context.Background(), pool, []model.VMRecommendation{rec}, nil))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vms/:vm_name/history", api.GetVMRecommendationHistory)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vms/api-hist-vm/history?cluster_uuid="+testutil.TestClusterUUID+"&namespace=api-ns&term=short_term&engine=performance&limit=10",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var resp struct {
		Data []engine.VMRecommendationHistoryRow `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.GreaterOrEqual(t, resp.Meta.Count, 1)
	assert.NotEmpty(t, resp.Data)
}
