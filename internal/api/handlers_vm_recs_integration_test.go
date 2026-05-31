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
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func seedVMRecCluster(t *testing.T, orgID string) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'vm-test', 1, NOW()) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID,
	)
	require.NoError(t, err)
}

func TestVMRecommendations_ListFilterAbandoned(t *testing.T) {
	orgID := "org-vm-abandoned-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	_ = config.GetConfig()

	seedVMRecCluster(t, orgID)

	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	now := time.Now().UTC()
	abandoned := model.VMRecommendation{
		OrgID:                orgID,
		ClusterUUID:          clusterID,
		VMName:               "abandoned-vm",
		Namespace:            "test-ns",
		GuestOS:              "linux",
		CurrentVCPU:          4,
		CurrentMemoryGiB:     8,
		RecommendedVCPU:      0,
		RecommendedMemoryGiB: 0,
		Confidence:           "high",
		Term:                 "short_term",
		Engine:               "cost",
		IsIdle:               false,
		IsAbandoned:          true,
		IsOversized:          true,
		Notifications:        []byte(`[{"code":43,"type":"critical","message":"abandoned"}]`),
		LastRecommendedAt:    now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	active := abandoned
	active.VMName = "active-vm"
	active.IsAbandoned = false
	active.IsIdle = true
	active.RecommendedVCPU = 1
	active.RecommendedMemoryGiB = 1
	active.Notifications = []byte(`[{"code":18,"type":"warning","message":"idle"}]`)

	require.NoError(t, engine.PersistVMRecommendations(context.Background(), pool, []model.VMRecommendation{abandoned, active}, nil))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vm", api.GetVMRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm?filter[is_abandoned]=true&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		Data []struct {
			VMName   string `json:"vm_name"`
			Metadata struct {
				IsAbandoned bool `json:"is_abandoned"`
			} `json:"metadata"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "abandoned-vm", resp.Data[0].VMName)
	assert.True(t, resp.Data[0].Metadata.IsAbandoned)
}
