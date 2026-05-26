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
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupTagsIntegrationApp(t *testing.T) (*echo.Echo, string, context.Context, func()) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	database.DB = gormDB
	database.Pool = pool

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'contract-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
		VALUES ($1, $2, $3, 'w1', 'Deployment', 'c1', '{"environment":"production","team":"platform"}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
		VALUES ($1, $2, 'other-ns', 'w2', 'Deployment', 'c2', '{"environment":"staging"}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_monthly_savings_usd, updated_at)
		VALUES ($1, $2, $3, 'w1', 'Deployment', 'c1', 'medium', 'cost', false, '{}', 10000, now())
		ON CONFLICT DO NOTHING`, testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_monthly_savings_usd, updated_at)
		VALUES ($1, $2, 'other-ns', 'w2', 'Deployment', 'c2', 'medium', 'cost', false, '{}', 20000, now())
		ON CONFLICT DO NOTHING`, testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	cleanup := func() {
		database.DB = nil
		database.Pool = nil
	}

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)
	v1.GET("/recommendations/openshift/nodes", api.GetNodeUtilizationRecs)
	v1.GET("/recommendations/openshift/savings-summary", api.GetFleetSavingsSummary)

	return app, makeIdentityHeader(testutil.TestOrgID), ctx, cleanup
}

func withTagsEnabled(t *testing.T) {
	t.Helper()
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "api")
	require.True(t, config.TagsFeatureEnabled())
}

func withTagsDisabled(t *testing.T) {
	t.Helper()
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "false")
	require.False(t, config.TagsFeatureEnabled())
}

func countProjects(body map[string]interface{}) int {
	data, _ := body["data"].([]interface{})
	projects := make(map[string]struct{})
	for _, raw := range data {
		item, _ := raw.(map[string]interface{})
		if p, ok := item["project"].(string); ok {
			projects[p] = struct{}{}
		}
	}
	return len(projects)
}

func TestTagFilters_ContainerList_BracketSyntax(t *testing.T) {
	withTagsEnabled(t)

	app, identity, _, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	url := "/api/cost-management/v1/recommendations/openshift?limit=50&filter%5Btag%3Aenvironment%5D=production"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 1, countProjects(body))
}

func TestTagFilters_ContainerList_FlatSyntax(t *testing.T) {
	withTagsEnabled(t)

	app, identity, _, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?limit=50&tag=environment:production", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 1, countProjects(body))
}

func TestTagFilters_IgnoredWhenDisabled(t *testing.T) {
	withTagsDisabled(t)

	app, identity, _, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?limit=50&filter%5Btag%3Aenvironment%5D=production", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.GreaterOrEqual(t, countProjects(body), 2)
}

func TestTagFilters_PVCList(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	_, err := database.Pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (org_id, cluster_uuid, namespace, persistentvolumeclaim, term, recommendation_type,
			capacity_bytes, usage_bytes_max, usage_ratio, notification_codes, data_days, estimated_monthly_savings_usd, updated_at)
		VALUES ($1, $2, $3, 'pvc-prod', 'medium', 'oversized', 1000, 100, 0.1, '{}', 7, 5000, now()),
		       ($1, $2, 'other-ns', 'pvc-stg', 'medium', 'oversized', 1000, 100, 0.1, '{}', 7, 6000, now())
		ON CONFLICT DO NOTHING`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/pvcs?filter%5Btag%3Aenvironment%5D=production", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	data, _ := body["data"].([]interface{})
	require.Len(t, data, 1)
	item := data[0].(map[string]interface{})
	assert.Equal(t, testutil.TestNamespace, item["namespace"])
}

func TestTagFilters_NodeUtilizationList(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	_, err := database.Pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, is_underutilized, is_overcommitted,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95, cpu_overcommit_ratio, pod_count, trend_slope,
			notification_codes, estimated_monthly_savings_usd, updated_at)
		VALUES ($1, $2, 'node-prod', 'medium', 'cost', true, false, 10, 20, 10, 20, 1, 1, 0, '{}', 30000, now())
		ON CONFLICT DO NOTHING`, testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/nodes?filter%5Btag%3Aenvironment%5D=production", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	data, _ := body["data"].([]interface{})
	require.NotEmpty(t, data)
}
