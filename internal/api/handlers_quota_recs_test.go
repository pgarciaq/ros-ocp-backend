package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestBpToPercentPtr(t *testing.T) {
	assert.Nil(t, bpToPercentPtr(sql.NullInt64{}))
	pct := bpToPercentPtr(sql.NullInt64{Int64: 2500, Valid: true})
	assert.NotNil(t, pct)
	assert.InDelta(t, 25.0, *pct, 0.001)
}

func TestQuotaValuesFromNull(t *testing.T) {
	assert.Nil(t, quotaValuesFromNull(sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{}))
	vals := quotaValuesFromNull(
		sql.NullInt64{Int64: 1000, Valid: true},
		sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{},
	)
	assert.NotNil(t, vals)
	assert.Equal(t, int64(1000), *vals.CPURequestMillicores)
}

func TestRegisterDisabledPluginRouteGuards_QuotaDisabled_Returns404(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()
	_ = config.GetConfig()

	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	registerDisabledPluginRouteGuards(v1)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/quota", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "not_found", body["status"])
	msg, ok := body["message"].(string)
	require.True(t, ok)
	require.Contains(t, msg, "plugin 'quota' is not enabled")
}

func setupQuotaRecommendationsHandler(t *testing.T, orgID string) *echo.Echo {
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
	v1.GET("/recommendations/openshift/quota", GetQuotaRecommendations)
	return e
}

func insertQuotaRecommendation(t *testing.T, orgID, clusterUUID, namespace string) {
	t.Helper()
	ctx := context.Background()
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO quota_recommendation_sets (
			org_id, cluster_uuid, namespace,
			cpu_request_hard_millicores, cpu_request_used_millicores,
			cpu_request_recommended_millicores,
			cpu_request_utilization_bp, recommendation_type, risk_level,
			last_observed_at
		) VALUES ($1, $2::uuid, $3, 100000, 25000, 36000, 2500, 'tighten', 'low', NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace) DO UPDATE SET
			recommendation_type = EXCLUDED.recommendation_type`,
		orgID, clusterUUID, namespace,
	)
	require.NoError(t, err)
}

func TestGetQuotaRecommendations_EmptyFleet_Returns200WithEmptyData(t *testing.T) {
	orgID := "org-quota-empty-" + uuid.New().String()[:8]
	e := setupQuotaRecommendationsHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/quota", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp QuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count)
	assert.NotNil(t, resp.Data)
	assert.Len(t, resp.Data, 0)
}

func TestGetQuotaRecommendations_WithData_Returns200(t *testing.T) {
	orgID := "org-quota-data-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440001"
	e := setupQuotaRecommendationsHandler(t, orgID)
	insertQuotaRecommendation(t, orgID, clusterUUID, "production")

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/quota", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp QuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, clusterUUID, resp.Data[0].ClusterUUID)
	assert.Equal(t, "production", resp.Data[0].Namespace)
	assert.Equal(t, "tighten", resp.Data[0].RecommendationType)
	assert.Equal(t, "low", resp.Data[0].RiskLevel)
	require.NotNil(t, resp.Data[0].QuotaHard)
	assert.Equal(t, int64(100000), *resp.Data[0].QuotaHard.CPURequestMillicores)
}

func TestGetQuotaRecommendations_FilterByCluster(t *testing.T) {
	orgID := "org-quota-filter-cluster-" + uuid.New().String()[:8]
	clusterA := "550e8400-e29b-41d4-a716-446655440010"
	clusterB := "550e8400-e29b-41d4-a716-446655440011"
	e := setupQuotaRecommendationsHandler(t, orgID)
	insertQuotaRecommendation(t, orgID, clusterA, "ns-a")
	insertQuotaRecommendation(t, orgID, clusterB, "ns-b")

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/quota?filter[cluster]="+clusterA, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp QuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	assert.Equal(t, clusterA, resp.Data[0].ClusterUUID)
}

func TestGetQuotaRecommendations_FilterByProject(t *testing.T) {
	orgID := "org-quota-filter-ns-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440020"
	e := setupQuotaRecommendationsHandler(t, orgID)
	insertQuotaRecommendation(t, orgID, clusterUUID, "target-ns")
	insertQuotaRecommendation(t, orgID, clusterUUID, "other-ns")

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/quota?filter[project]=target-ns", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp QuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	assert.Equal(t, "target-ns", resp.Data[0].Namespace)
}

func TestGetQuotaRecommendations_OrgIsolation(t *testing.T) {
	orgA := "org-quota-a-" + uuid.New().String()[:8]
	orgB := "org-quota-b-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440030"

	eA := setupQuotaRecommendationsHandler(t, orgA)
	insertQuotaRecommendation(t, orgA, clusterUUID, "shared-ns")

	pool := database.Pool
	eB := echo.New()
	eB.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("Identity", identity.XRHID{Identity: identity.Identity{OrgID: orgB}})
			c.Set("user.permissions", map[string][]string{"*": {}})
			return next(c)
		}
	})
	v1 := eB.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/quota", GetQuotaRecommendations)
	database.Pool = pool

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/quota", nil)
	rec := httptest.NewRecorder()
	eB.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp QuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count)

	// org A still sees its row
	reqA := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/quota", nil)
	recA := httptest.NewRecorder()
	eA.ServeHTTP(recA, reqA)
	require.Equal(t, http.StatusOK, recA.Code)
	var respA QuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(recA.Body.Bytes(), &respA))
	assert.Equal(t, 1, respA.Meta.Count)
}

func TestGetQuotaRecommendations_Unauthorized_Returns401(t *testing.T) {
	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/quota", GetQuotaRecommendations)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/quota", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetQuotaRecommendations_OrderByUtilizationDesc(t *testing.T) {
	orgID := "org-quota-order-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440040"
	e := setupQuotaRecommendationsHandler(t, orgID)
	ctx := context.Background()
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO quota_recommendation_sets (
			org_id, cluster_uuid, namespace,
			cpu_request_hard_millicores, cpu_request_utilization_bp,
			recommendation_type, risk_level, last_observed_at
		) VALUES ($1, $2::uuid, 'low-util', 100000, 1000, 'optimal', 'low', NOW()),
		       ($1, $2::uuid, 'high-util', 100000, 9000, 'raise', 'high', NOW())`,
		orgID, clusterUUID,
	)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/quota?order_by=utilization&order_how=desc", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp QuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 2)
	assert.Equal(t, "high-util", resp.Data[0].Namespace)
}

func TestGetQuotaRecommendations_InvalidOrderBy(t *testing.T) {
	orgID := "org-quota-bad-order-" + uuid.New().String()[:8]
	e := setupQuotaRecommendationsHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/quota?order_by=invalid", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetQuotaRecommendationDetail_ReturnsHistory(t *testing.T) {
	orgID := "org-quota-detail-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440050"
	e := setupQuotaRecommendationsHandler(t, orgID)
	insertQuotaRecommendation(t, orgID, clusterUUID, "detail-ns")

	ctx := context.Background()
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO quota_recommendation_history (
			org_id, cluster_uuid, namespace, recommendation_type, risk_level, recorded_at
		) VALUES ($1, $2::uuid, 'detail-ns', 'tighten', 'low', NOW() - INTERVAL '2 days')`,
		orgID, clusterUUID,
	)
	require.NoError(t, err)

	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/quota/detail", GetQuotaRecommendationDetail)

	url := "/api/cost-management/v1/recommendations/openshift/quota/detail" +
		"?cluster_uuid=" + clusterUUID + "&namespace=detail-ns"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var detail QuotaRecommendationDetailResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
	assert.Equal(t, "detail-ns", detail.Namespace)
	assert.Equal(t, "tighten", detail.RecommendationType)
	require.Len(t, detail.History, 1)
}

func TestQuotaUtilFromNullBP(t *testing.T) {
	assert.Nil(t, quotaUtilFromNullBP(sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{}))
	util := quotaUtilFromNullBP(
		sql.NullInt64{Int64: 5000, Valid: true},
		sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{},
	)
	require.NotNil(t, util)
	require.NotNil(t, util.CPURequestPercent)
	assert.InDelta(t, 50.0, *util.CPURequestPercent, 0.001)
}
