package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	v1.GET("/recommendations/openshift/cluster-quota/detail", GetClusterQuotaRecommendationDetail)
	return e
}

type clusterQuotaRecOpts struct {
	recommendationType string
	riskLevel          string
	namespaces         string
	cpuUtilPercent     int
	savingsDollars     int64
	notificationCodes  []int16
}

func insertClusterQuotaRecommendationWithOpts(
	t *testing.T,
	orgID, clusterUUID, crqName string,
	opts clusterQuotaRecOpts,
) {
	t.Helper()
	if opts.recommendationType == "" {
		opts.recommendationType = "tighten"
	}
	if opts.riskLevel == "" {
		opts.riskLevel = "low"
	}
	if opts.notificationCodes == nil {
		opts.notificationCodes = []int16{}
	}
	ctx := context.Background()
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO cluster_quota_recommendation_sets (
			org_id, cluster_uuid, cluster_quota_name,
			cpu_request_hard, cpu_request_used, cpu_request_recommended,
			recommendation_type, risk_level, namespaces,
			utilization_cpu_request_percent,
			savings_cpu_cores_freed, savings_memory_bytes_freed,
			savings_storage_bytes_freed, savings_pods_freed,
			savings_dollars_monthly, notification_codes
		) VALUES ($1, $2::uuid, $3, 100000, 25000, 36000, $4, $5, $6, $7,
			2, 1073741824, 5368709120, 5, $8, $9)
		ON CONFLICT (org_id, cluster_uuid, cluster_quota_name) DO UPDATE SET
			recommendation_type = EXCLUDED.recommendation_type,
			risk_level = EXCLUDED.risk_level,
			namespaces = EXCLUDED.namespaces,
			utilization_cpu_request_percent = EXCLUDED.utilization_cpu_request_percent,
			savings_dollars_monthly = EXCLUDED.savings_dollars_monthly,
			notification_codes = EXCLUDED.notification_codes`,
		orgID, clusterUUID, crqName,
		opts.recommendationType, opts.riskLevel, opts.namespaces, opts.cpuUtilPercent,
		opts.savingsDollars, opts.notificationCodes,
	)
	require.NoError(t, err)
}

func insertClusterQuotaHistory(t *testing.T, orgID, clusterUUID, crqName, resource string) {
	t.Helper()
	ctx := context.Background()
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO cluster_quota_recommendation_history (
			org_id, cluster_uuid, cluster_quota_name,
			resource, recommendation_type, risk_level,
			recommended_hard, current_hard, current_used, utilization_percent
		) VALUES ($1, $2::uuid, $3, $4, 'tighten', 'low', 36000, 100000, 25000, 25)`,
		orgID, clusterUUID, crqName, resource,
	)
	require.NoError(t, err)
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

func TestGetClusterQuotaRecommendations_FormatCSV(t *testing.T) {
	orgID := "org-crq-csv-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440002"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendation(t, orgID, clusterUUID, "csv-crq", 10)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?format=csv&limit=100", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/csv")

	reader := csv.NewReader(strings.NewReader(rec.Body.String()))
	rows, err := reader.ReadAll()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 2)
	assert.Equal(t, "cluster_uuid", rows[0][0])
	assert.Equal(t, "cluster_quota_name", rows[0][1])
	assert.Equal(t, clusterUUID, rows[1][0])
	assert.Equal(t, "csv-crq", rows[1][1])
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

func TestGetClusterQuotaRecommendations_FilterByClusterQuotaName(t *testing.T) {
	orgID := "org-crq-filter-name-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440020"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendation(t, orgID, clusterUUID, "target-crq", 10)
	insertClusterQuotaRecommendation(t, orgID, clusterUUID, "other-crq", 20)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?filter[cluster_quota_name]=target-crq", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	assert.Equal(t, "target-crq", resp.Data[0].ClusterQuotaName)
}

func TestGetClusterQuotaRecommendations_FilterCrqAlias(t *testing.T) {
	orgID := "org-crq-filter-crq-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440021"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendation(t, orgID, clusterUUID, "crq-alias", 10)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?filter[crq]=crq-alias", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	assert.Equal(t, "crq-alias", resp.Data[0].ClusterQuotaName)
}

func TestGetClusterQuotaRecommendations_FilterProjectAlias(t *testing.T) {
	orgID := "org-crq-filter-project-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440041"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "project-crq", clusterQuotaRecOpts{
		namespaces: "team-a, team-b",
	})
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "other-crq", clusterQuotaRecOpts{
		namespaces: "team-c",
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?filter[project]=team-a", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	assert.Equal(t, "project-crq", resp.Data[0].ClusterQuotaName)
}

func TestGetClusterQuotaRecommendations_CapacityFreedAllKeys(t *testing.T) {
	orgID := "org-crq-capacity-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440071"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendation(t, orgID, clusterUUID, "capacity-crq", 42)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.NotNil(t, resp.Data[0].CapacityFreed)
	assert.Equal(t, int64(2), resp.Data[0].CapacityFreed.CPUCoresFreed)
	assert.Equal(t, int64(1073741824), resp.Data[0].CapacityFreed.MemoryBytes)
	assert.Equal(t, int64(5368709120), resp.Data[0].CapacityFreed.StorageRequestBytes)
	assert.Equal(t, int64(5), resp.Data[0].CapacityFreed.PodsFreed)
}

func TestGetClusterQuotaRecommendations_NamespacesArray(t *testing.T) {
	orgID := "org-crq-namespaces-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440072"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "ns-crq", clusterQuotaRecOpts{
		namespaces: "alpha-ns, beta-ns",
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, []string{"alpha-ns", "beta-ns"}, resp.Data[0].Namespaces)
}

func TestGetClusterQuotaRecommendations_FilterClusterResourceQuotaAlias(t *testing.T) {
	orgID := "org-crq-filter-alias-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440021"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendation(t, orgID, clusterUUID, "alias-crq", 10)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?filter[cluster_resource_quota]=alias-crq", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	assert.Equal(t, "alias-crq", resp.Data[0].ClusterQuotaName)
}

func TestGetClusterQuotaRecommendations_FilterByCluster(t *testing.T) {
	orgID := "org-crq-filter-cluster-" + uuid.New().String()[:8]
	clusterA := "550e8400-e29b-41d4-a716-446655440030"
	clusterB := "550e8400-e29b-41d4-a716-446655440031"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendation(t, orgID, clusterA, "crq-a", 10)
	insertClusterQuotaRecommendation(t, orgID, clusterB, "crq-b", 20)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?filter[cluster]="+clusterA, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	assert.Equal(t, clusterA, resp.Data[0].ClusterUUID)
}

func TestGetClusterQuotaList_FilterTag(t *testing.T) {
	orgID := "org-crq-tag-" + uuid.New().String()[:8]
	clusterUUID := testutil.TestClusterUUID
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "api")
	require.True(t, config.TagsFeatureEnabled())

	ctx := context.Background()

	_, err := database.Pool.Exec(ctx, `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
		VALUES ($1, $2, $3, 'w1', 'Deployment', 'c1', '{"environment":"production"}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
		orgID, clusterUUID, testutil.TestNamespace)
	require.NoError(t, err)
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
		VALUES ($1, $2, 'other-ns', 'w2', 'Deployment', 'c2', '{"environment":"staging"}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
		orgID, clusterUUID)
	require.NoError(t, err)

	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "crq-prod", clusterQuotaRecOpts{namespaces: testutil.TestNamespace})
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "crq-other", clusterQuotaRecOpts{namespaces: "other-ns"})

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?filter%5Btag%3Aenvironment%5D=production", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "crq-prod", resp.Data[0].ClusterQuotaName)
}

func TestGetClusterQuotaRecommendations_FilterByNamespace(t *testing.T) {
	orgID := "org-crq-filter-ns-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440040"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "ns-crq", clusterQuotaRecOpts{
		namespaces: "team-a, team-b",
	})
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "other-crq", clusterQuotaRecOpts{
		namespaces: "team-c",
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?filter[namespace]=team-a", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	assert.Equal(t, "ns-crq", resp.Data[0].ClusterQuotaName)
}

func TestGetClusterQuotaRecommendations_FilterRecommendationTypeAndRiskLevel(t *testing.T) {
	orgID := "org-crq-filter-type-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440050"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "high-raise", clusterQuotaRecOpts{
		recommendationType: "raise",
		riskLevel:          "high",
	})
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "low-tighten", clusterQuotaRecOpts{
		recommendationType: "tighten",
		riskLevel:          "low",
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?filter[recommendation_type]=raise&filter[risk_level]=high", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	assert.Equal(t, "high-raise", resp.Data[0].ClusterQuotaName)
	assert.Equal(t, "raise", resp.Data[0].RecommendationType)
	assert.Equal(t, "high", resp.Data[0].RiskLevel)
}

func TestGetClusterQuotaRecommendations_OrderByRiskLevelDesc(t *testing.T) {
	orgID := "org-crq-order-risk-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440060"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "low-crq", clusterQuotaRecOpts{riskLevel: "low"})
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "high-crq", clusterQuotaRecOpts{riskLevel: "high"})
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "medium-crq", clusterQuotaRecOpts{riskLevel: "medium"})

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?order_by=risk_level&order_how=desc", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 3)
	assert.Equal(t, "high", resp.Data[0].RiskLevel)
	assert.Equal(t, "medium", resp.Data[1].RiskLevel)
	assert.Equal(t, "low", resp.Data[2].RiskLevel)
}

func TestGetClusterQuotaRecommendations_OrderByClusterQuotaNameAsc(t *testing.T) {
	orgID := "org-crq-order-name-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440065"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendation(t, orgID, clusterUUID, "zebra-quota", 10)
	insertClusterQuotaRecommendation(t, orgID, clusterUUID, "alpha-quota", 20)
	insertClusterQuotaRecommendation(t, orgID, clusterUUID, "middle-quota", 30)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?order_by=cluster_quota_name&order_how=asc", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 3)
	assert.Equal(t, "alpha-quota", resp.Data[0].ClusterQuotaName)
	assert.Equal(t, "middle-quota", resp.Data[1].ClusterQuotaName)
	assert.Equal(t, "zebra-quota", resp.Data[2].ClusterQuotaName)
}

func TestGetClusterQuotaRecommendations_OrderByUtilizationDesc(t *testing.T) {
	orgID := "org-crq-order-util-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440066"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "low-util", clusterQuotaRecOpts{cpuUtilPercent: 25})
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "high-util", clusterQuotaRecOpts{cpuUtilPercent: 95})
	insertClusterQuotaRecommendationWithOpts(t, orgID, clusterUUID, "mid-util", clusterQuotaRecOpts{cpuUtilPercent: 60})

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?order_by=utilization&order_how=desc", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 3)
	assert.Equal(t, "high-util", resp.Data[0].ClusterQuotaName)
	assert.Equal(t, "mid-util", resp.Data[1].ClusterQuotaName)
	assert.Equal(t, "low-util", resp.Data[2].ClusterQuotaName)
}

func TestGetClusterQuotaRecommendations_OrderByEstimatedMonthlySavingsDesc(t *testing.T) {
	orgID := "org-crq-order-savings-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440070"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendation(t, orgID, clusterUUID, "small-savings", 10)
	insertClusterQuotaRecommendation(t, orgID, clusterUUID, "large-savings", 100)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?order_by=estimated_monthly_savings&order_how=desc", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp ClusterQuotaRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 2)
	assert.Equal(t, "large-savings", resp.Data[0].ClusterQuotaName)
	assert.Equal(t, 100, resp.Data[0].EstimatedSavings.Value)
}

func TestGetClusterQuotaRecommendationDetail_ReturnsHistory(t *testing.T) {
	orgID := "org-crq-detail-" + uuid.New().String()[:8]
	clusterUUID := "550e8400-e29b-41d4-a716-446655440080"
	crqName := "detail-crq"
	e := setupClusterQuotaRecommendationsHandler(t, orgID)
	insertClusterQuotaRecommendation(t, orgID, clusterUUID, crqName, 55)
	insertClusterQuotaHistory(t, orgID, clusterUUID, crqName, "cpu_request")

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota/detail?cluster_uuid="+clusterUUID+"&cluster_quota_name="+crqName, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var detail ClusterQuotaRecommendationDetailResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
	assert.Equal(t, clusterUUID, detail.ClusterUUID)
	assert.Equal(t, crqName, detail.ClusterQuotaName)
	require.NotEmpty(t, detail.History)
	assert.Equal(t, "cpu_request", detail.History[0].Resource)
}

func TestGetClusterQuotaRecommendationDetail_NotFound(t *testing.T) {
	orgID := "org-crq-detail-miss-" + uuid.New().String()[:8]
	e := setupClusterQuotaRecommendationsHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota/detail?cluster_uuid=550e8400-e29b-41d4-a716-446655440099&cluster_quota_name=missing", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}
