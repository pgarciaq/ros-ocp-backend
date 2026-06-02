package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupClusterQuotaRecommendationsHandler(t *testing.T, orgID string) *echo.Echo {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()
	_ = config.GetConfig()

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("Identity", identity.XRHID{
				Identity: identity.Identity{OrgID: orgID},
			})
			c.Set("user.permissions", map[string][]string{"*": {}})
			return next(c)
		}
	})
	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/cluster-quota", GetClusterQuotaRecommendations)
	return e
}

func insertClusterQuotaRecommendation(t *testing.T, orgID, clusterUUID, crqName string, savingsDollars int64) {
	t.Helper()
	ctx := context.Background()
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO cluster_quota_recommendation_sets (
			org_id, cluster_uuid, cluster_quota_name,
			cpu_request_hard, cpu_request_used, cpu_request_recommended,
			recommendation_type, risk_level,
			savings_cpu_cores_freed, savings_memory_bytes_freed,
			savings_storage_bytes_freed, savings_pods_freed,
			savings_dollars_monthly
		) VALUES ($1, $2::uuid, $3, 100000, 25000, 36000, 'tighten', 'low',
			2, 1073741824, 5368709120, 5, $4)
		ON CONFLICT (org_id, cluster_uuid, cluster_quota_name) DO UPDATE SET
			recommendation_type = EXCLUDED.recommendation_type,
			savings_dollars_monthly = EXCLUDED.savings_dollars_monthly`,
		orgID, clusterUUID, crqName, savingsDollars,
	)
	require.NoError(t, err)
}

func TestGetClusterQuotaRecommendations_EmptyFleet_Returns200WithEmptyData(t *testing.T) {
	orgID := "org-crq-empty-" + uuid.New().String()[:8]
	e := setupClusterQuotaRecommendationsHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/cluster-quota", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count)
	assert.NotNil(t, resp.Data)
	assert.Len(t, resp.Data, 0)
}

func TestGetClusterQuotaRecommendations_WithData_Returns200(t *testing.T) {
	orgID := "org-crq-data-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440001"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendation(t, orgID, clusterUUID, "team-quota", 42)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/cluster-quota", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, clusterUUID, resp.Data[0].ClusterUUID)
	assert.Equal(t, "team-quota", resp.Data[0].ClusterQuotaName)
	assert.Equal(t, "tighten", resp.Data[0].RecommendationType)
	require.NotNil(t, resp.Data[0].EstimatedSavings)
	assert.Equal(t, 42, resp.Data[0].EstimatedSavings.Value)
}

func TestGetClusterQuotaRecommendations_GroupByCluster(t *testing.T) {
	orgID := "org-crq-group-" + uuid.New().String()[:8]
	clusterA := "550e8400-e29b-41d4-a716-446655440010"
	clusterB := "550e8400-e29b-41d4-a716-446655440011"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendation(t, orgID, clusterA, "crq-a1", 10)
	insertClusterQuotaRecommendation(t, orgID, clusterA, "crq-a2", 20)
	insertClusterQuotaRecommendation(t, orgID, clusterB, "crq-b1", 5)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?group_by[cluster]=true", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Meta.Count)
	require.Len(t, resp.Data, 2)

	byCluster := map[string]ClusterQuotaRecommendationListItem{}
	for _, item := range resp.Data {
		byCluster[item.ClusterUUID] = item
	}

	require.Contains(t, byCluster, clusterA)
	assert.Equal(t, 2, byCluster[clusterA].Count)
	require.NotNil(t, byCluster[clusterA].EstimatedSavings)
	assert.Equal(t, 30, byCluster[clusterA].EstimatedSavings.Value)
	require.NotNil(t, byCluster[clusterA].CapacityFreed)
	assert.Equal(t, int64(4), byCluster[clusterA].CapacityFreed.CPUCoresFreed)

	require.Contains(t, byCluster, clusterB)
	assert.Equal(t, 1, byCluster[clusterB].Count)
	require.NotNil(t, byCluster[clusterB].EstimatedSavings)
	assert.Equal(t, 5, byCluster[clusterB].EstimatedSavings.Value)
}
