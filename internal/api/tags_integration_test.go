package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupTagsIntegrationApp(t *testing.T) (*echo.Echo, string, context.Context, func()) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
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
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_savings_cents, updated_at)
		VALUES ($1, $2, $3, 'w1', 'Deployment', 'c1', 'medium', 'cost', false, '{}', 10000, now())
		ON CONFLICT DO NOTHING`, testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_savings_cents, updated_at)
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
	v1.GET("/recommendations/openshift/namespaces", api.GetNamespaceRecommendationSetListWithFallback)
	v1.GET("/recommendations/openshift/savings-summary", api.GetFleetSavingsSummary)
	v1.GET("/recommendations/openshift/vm", api.GetVMRecommendations)
	v1.GET("/recommendations/openshift/quota", api.GetQuotaRecommendations)
	v1.GET("/recommendations/openshift/cluster-quota", api.GetClusterQuotaRecommendations)
	v1.GET("/recommendations/openshift/history", api.GetRecommendationHistory)
	v1.GET("/recommendations/openshift/gpu/timeslicing", api.GetNodeRecommendations)

	return app, makeIdentityHeader(testutil.TestOrgID), ctx, cleanup
}

func seedKokuTagValuesForFilter(t *testing.T, ctx context.Context) {
	t.Helper()
	schema, err := tags.TenantSchema(testutil.TestOrgID)
	require.NoError(t, err)
	_, err = database.Pool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS `+schema+`;
		CREATE TABLE IF NOT EXISTS `+schema+`.reporting_ocptags_values (
			uuid UUID PRIMARY KEY,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			cluster_ids TEXT[] NOT NULL,
			cluster_aliases TEXT[] NOT NULL DEFAULT '{}',
			namespaces TEXT[] NOT NULL,
			nodes TEXT[],
			UNIQUE (key, value)
		);
	`)
	require.NoError(t, err)
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO `+schema+`.reporting_ocptags_values (
			uuid, key, value, cluster_ids, cluster_aliases, namespaces
		) VALUES ($1, 'environment', 'production', ARRAY[$2], ARRAY['alias'], ARRAY[$3])
		ON CONFLICT DO NOTHING`,
		uuid.New(), testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)
}

func seedTagFilterWorkloadKeys(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
		VALUES ($1, $2, $3, 'w1', 'Deployment', 'c1', '{"environment":"production","team":"platform"}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
		VALUES ($1, $2, 'other-ns', 'w2', 'Deployment', 'c2', '{"environment":"staging"}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)
}

func withTagsEnabled(t *testing.T) {
	t.Helper()
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "api")
	t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-token")
	require.True(t, config.TagsFeatureEnabled())
}

func withTagsDBEnabled(t *testing.T) {
	t.Helper()
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")
	t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-token")
	require.True(t, config.TagsFeatureEnabled())
	require.Equal(t, "db", config.TagsSource())
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

func seedTagFilterMultiKeyWorkloads(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
		VALUES ($1, $2, $3, 'w1', 'Deployment', 'c1', '{"environment":"production","team":"platform"}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
		VALUES ($1, $2, 'prod-other-team', 'w3', 'Deployment', 'c3', '{"environment":"production","team":"billing"}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
		VALUES ($1, $2, 'other-ns', 'w2', 'Deployment', 'c2', '{"environment":"staging"}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	for _, ns := range []string{testutil.TestNamespace, "prod-other-team", "other-ns"} {
		wl, cn := "w1", "c1"
		if ns == "prod-other-team" {
			wl, cn = "w3", "c3"
		} else if ns == "other-ns" {
			wl, cn = "w2", "c2"
		}
		_, err = database.Pool.Exec(ctx, `
			INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_savings_cents, updated_at)
			VALUES ($1, $2, $3, $4, 'Deployment', $5, 'medium', 'cost', false, '{}', 10000, now())
			ON CONFLICT DO NOTHING`,
			testutil.TestOrgID, testutil.TestClusterUUID, ns, wl, cn)
		require.NoError(t, err)
	}
}

func TestTagFilters_MultiKeyAND_NarrowsResults(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()
	seedTagFilterMultiKeyWorkloads(t, ctx)

	envOnlyURL := "/api/cost-management/v1/recommendations/openshift?limit=50&filter%5Btag%3Aenvironment%5D=production"
	reqEnv := httptest.NewRequest(http.MethodGet, envOnlyURL, nil)
	reqEnv.Header.Set("X-Rh-Identity", identity)
	recEnv := httptest.NewRecorder()
	app.ServeHTTP(recEnv, reqEnv)
	require.Equal(t, http.StatusOK, recEnv.Code, recEnv.Body.String())

	var envBody map[string]interface{}
	require.NoError(t, json.Unmarshal(recEnv.Body.Bytes(), &envBody))
	envCount := countProjects(envBody)
	require.Equal(t, 2, envCount, "production tag should match two namespaces")

	dualURL := "/api/cost-management/v1/recommendations/openshift?limit=50" +
		"&filter%5Btag%3Aenvironment%5D=production&filter%5Btag%3Ateam%5D=platform"
	reqDual := httptest.NewRequest(http.MethodGet, dualURL, nil)
	reqDual.Header.Set("X-Rh-Identity", identity)
	recDual := httptest.NewRecorder()
	app.ServeHTTP(recDual, reqDual)

	require.Equal(t, http.StatusOK, recDual.Code, recDual.Body.String())
	var dualBody map[string]interface{}
	require.NoError(t, json.Unmarshal(recDual.Body.Bytes(), &dualBody))
	dualCount := countProjects(dualBody)
	assert.Equal(t, 1, dualCount)
	assert.Less(t, dualCount, envCount, "AND across tag keys should narrow results")

	data, _ := dualBody["data"].([]interface{})
	require.NotEmpty(t, data)
	item := data[0].(map[string]interface{})
	assert.Equal(t, testutil.TestNamespace, item["project"])
}

func TestTagFilters_WithRBACScope_Intersection(t *testing.T) {
	withTagsEnabled(t)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})

	clusterAllowed := testutil.TestClusterUUID
	clusterDenied := "b2222222-2222-2222-2222-222222222222"

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'tag-rbac-allowed', 'src-1', now()), (1, $2, 'tag-rbac-denied', 'src-2', now()) ON CONFLICT DO NOTHING`,
		clusterAllowed, clusterDenied)
	require.NoError(t, err)

	for _, cl := range []struct{ uuid, ns string }{
		{clusterAllowed, testutil.TestNamespace},
		{clusterDenied, "denied-ns"},
	} {
		_, err = pool.Exec(ctx, `
			INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
			VALUES ($1, $2, $3, 'rbac-w', 'Deployment', 'rbac-c', '{"environment":"production"}'::jsonb)
			ON CONFLICT (org_id, namespace, workload, container_name)
			DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
			testutil.TestOrgID, cl.uuid, cl.ns)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `
			INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_savings_cents, updated_at)
			VALUES ($1, $2, $3, 'rbac-w', 'Deployment', 'rbac-c', 'medium', 'cost', false, '{}', 10000, now())
			ON CONFLICT DO NOTHING`, testutil.TestOrgID, cl.uuid, cl.ns)
		require.NoError(t, err)
	}

	cfg := config.GetConfig()
	origRBAC := cfg.RBACEnabled
	cfg.RBACEnabled = true
	t.Cleanup(func() { cfg.RBACEnabled = origRBAC })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user.permissions", map[string][]string{
				"openshift.cluster": {clusterAllowed},
				"openshift.project": {"*"},
			})
			return next(c)
		}
	})
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identity := makeIdentityHeader(testutil.TestOrgID)
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?limit=50&filter%5Btag%3Aenvironment%5D=production", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 1, countProjects(body))

	data, _ := body["data"].([]interface{})
	require.Len(t, data, 1)
	item := data[0].(map[string]interface{})
	assert.Equal(t, clusterAllowed, item["cluster_uuid"])
	assert.Equal(t, testutil.TestNamespace, item["project"])
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
			capacity_bytes, usage_bytes_max, usage_ratio, notification_codes, data_days, estimated_savings_cents, updated_at)
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
			notification_codes, estimated_savings_cents, updated_at)
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

func TestTagFilters_NodeUtilizationList_DBSource(t *testing.T) {
	withTagsDBEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()
	seedKokuTagValuesForFilter(t, ctx)

	_, err := database.Pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, is_underutilized, is_overcommitted,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95, cpu_overcommit_ratio, pod_count, trend_slope,
			notification_codes, estimated_savings_cents, updated_at)
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

func TestTagFilters_NamespaceList(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	monEnd := time.Now().UTC()
	monStart := monEnd.Add(-7 * 24 * time.Hour)
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO namespace_recommendation_sets (
			org_id, cluster_uuid, namespace_name, term, engine, schedule_type, stale,
			rec_cpu_request_millicores, rec_cpu_limit_millicores,
			rec_memory_request_kib, rec_memory_limit_kib,
			current_cpu_request_millicores, current_cpu_limit_millicores,
			current_memory_request_kib, current_memory_limit_kib,
			variation_cpu_request_pct, variation_cpu_limit_pct,
			variation_memory_request_pct, variation_memory_limit_pct,
			confidence_level, notification_codes, monitoring_start_time, monitoring_end_time, updated_at
		) VALUES
			($1, $2, $3, 'medium', 'cost', 'all_hours', false,
			 4000, 8000, 8388608, 16777216, 5000, 10000, 10485760, 20971520,
			 -10, -10, -10, -10, 0.9, '{}', $5, $4, NOW()),
			($1, $2, 'other-ns', 'medium', 'cost', 'all_hours', false,
			 4000, 8000, 8388608, 16777216, 5000, 10000, 10485760, 20971520,
			 -10, -10, -10, -10, 0.9, '{}', $5, $4, NOW())
		ON CONFLICT DO NOTHING`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace, monEnd, monStart)
	require.NoError(t, err)

	_, err = database.Pool.Exec(ctx, `
		INSERT INTO org_namespace_keys (org_id, cluster_uuid, namespace_name, last_reported, resolved_tags)
		VALUES ($1, $2, $3, NOW(), '{"environment":"production","team":"platform"}'::jsonb),
			($1, $2, 'other-ns', NOW(), '{"environment":"staging"}'::jsonb)
		ON CONFLICT (org_id, cluster_uuid, namespace_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/namespaces?filter%5Btag%3Aenvironment%5D=production", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	data, _ := body["data"].([]interface{})
	require.Len(t, data, 1)
	item := data[0].(map[string]interface{})
	assert.Equal(t, testutil.TestNamespace, item["project"])
}

func TestTagFilters_VMList(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()
	seedTagFilterWorkloadKeys(t, ctx)

	_, err := database.Pool.Exec(ctx, `
		INSERT INTO vm_recommendations (
			org_id, cluster_uuid, vm_name, namespace, guest_os,
			current_vcpu, current_memory_gib, recommended_vcpu, recommended_memory_gib,
			guest_agent_detected, confidence, term, engine,
			is_idle, is_abandoned, is_oversized, estimated_savings_cents, savings_currency, last_recommended_at
		) VALUES (
			$1, $2, 'vm-prod', $3, 'linux',
			4, 16, 2, 8,
			true, 'high', 'medium_term', 'cost',
			false, false, false, 10.00, 'USD', now()),
			($1, $2, 'vm-stg', 'other-ns', 'linux',
			4, 16, 2, 8,
			true, 'high', 'medium_term', 'cost',
			false, false, false, 20.00, 'USD', now())`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm?filter%5Btag%3Aenvironment%5D=production", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	meta, _ := body["meta"].(map[string]interface{})
	require.NotNil(t, meta)
	count, _ := meta["count"].(float64)
	assert.Equal(t, float64(1), count)
	data, _ := body["data"].([]interface{})
	require.Len(t, data, 1)
	item := data[0].(map[string]interface{})
	assert.Equal(t, testutil.TestNamespace, item["namespace"])
}

func TestTagFilters_QuotaList(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()
	seedTagFilterWorkloadKeys(t, ctx)

	_, err := database.Pool.Exec(ctx, `
		INSERT INTO quota_recommendation_sets (
			org_id, cluster_uuid, namespace,
			cpu_request_hard_millicores, cpu_request_used_millicores,
			cpu_request_recommended_millicores,
			cpu_request_utilization_bp, recommendation_type, risk_level,
			last_observed_at
		) VALUES ($1, $2, $3, 100000, 25000, 36000, 2500, 'tighten', 'low', NOW()),
		       ($1, $2, 'other-ns', 100000, 25000, 36000, 2500, 'tighten', 'low', NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, quota_name) DO UPDATE SET
			recommendation_type = EXCLUDED.recommendation_type`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/quota?filter%5Btag%3Aenvironment%5D=production", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	meta, _ := body["meta"].(map[string]interface{})
	count, _ := meta["count"].(float64)
	assert.Equal(t, float64(1), count)
	data, _ := body["data"].([]interface{})
	require.Len(t, data, 1)
	item := data[0].(map[string]interface{})
	assert.Equal(t, testutil.TestNamespace, item["namespace"])
}

func TestTagFilters_ClusterQuotaList(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()
	seedTagFilterWorkloadKeys(t, ctx)

	_, err := database.Pool.Exec(ctx, `
		INSERT INTO cluster_quota_recommendation_sets (
			org_id, cluster_uuid, cluster_quota_name,
			cpu_request_hard, cpu_request_used, cpu_request_recommended,
			recommendation_type, risk_level, namespaces,
			utilization_cpu_request_percent,
			savings_cpu_cores_freed, savings_memory_bytes_freed,
			savings_storage_bytes_freed, savings_pods_freed,
			estimated_savings_cents, notification_codes
		) VALUES ($1, $2, 'crq-prod', 100000, 25000, 36000, 'tighten', 'low', $3, 25,
			2, 1073741824, 5368709120, 5, 4200, '{}'),
		       ($1, $2, 'crq-other', 100000, 25000, 36000, 'tighten', 'low', 'other-ns', 25,
			2, 1073741824, 5368709120, 5, 2000, '{}')
		ON CONFLICT (org_id, cluster_uuid, cluster_quota_name) DO UPDATE SET
			namespaces = EXCLUDED.namespaces`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/cluster-quota?filter%5Btag%3Aenvironment%5D=production", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	meta, _ := body["meta"].(map[string]interface{})
	count, _ := meta["count"].(float64)
	assert.Equal(t, float64(1), count)
	data, _ := body["data"].([]interface{})
	require.Len(t, data, 1)
	item := data[0].(map[string]interface{})
	assert.Equal(t, "crq-prod", item["cluster_quota_name"])
}

func TestTagFilters_HistoryList(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()
	seedTagFilterWorkloadKeys(t, ctx)

	recordedAt := time.Now().UTC().Truncate(24 * time.Hour)
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO recommendation_history (
			recorded_at, org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			term, engine, rec_cpu_request_millicores, rec_cpu_limit_millicores,
			rec_memory_request_kib, rec_memory_limit_kib, notification_codes, confidence_level,
			estimated_savings_cents, source_binary
		) VALUES
			($1, $2, $3, $4, 'w1', 'Deployment', 'c1', 'medium', 'cost', 100, 200, 1024, 2048, '{}', 0.9, 10000, 'test'),
			($1, $2, $3, 'other-ns', 'w2', 'Deployment', 'c2', 'medium', 'cost', 100, 200, 1024, 2048, '{}', 0.9, 20000, 'test')
		ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, recorded_at)
		DO NOTHING`,
		recordedAt, testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/history?filter%5Btag%3Aenvironment%5D=production&limit=50", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	meta, _ := body["meta"].(map[string]interface{})
	count, _ := meta["count"].(float64)
	assert.Equal(t, float64(1), count)
	data, _ := body["data"].([]interface{})
	require.Len(t, data, 1)
	item := data[0].(map[string]interface{})
	assert.Equal(t, testutil.TestNamespace, item["namespace"])
}

// seedTagFilterGPUTimeslicingNode seeds underutilized GPU digests on one node so the
// time-slicing engine emits a recommendation for containers in the given namespace.
func seedTagFilterGPUTimeslicingNode(
	t *testing.T,
	pool *pgxpool.Pool,
	namespace string,
	workloads [3]struct{ wl, cn string },
	nodeName string,
) {
	t.Helper()
	start := testutil.RecentStart()
	smAvgs := [3]float64{0.12, 0.08, 0.15}
	for i, w := range workloads {
		for day := 0; day < 7; day++ {
			testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
				IntervalStart:       start.AddDate(0, 0, day),
				ClusterUUID:         testutil.TestClusterUUID,
				Namespace:           namespace,
				Workload:            w.wl,
				WorkloadType:        "deployment",
				ContainerName:       w.cn,
				GPUModelName:        "NVIDIA T4",
				NodeName:            nodeName,
				FBUsageMinMiB:       500,
				FBUsageMaxMiB:       2000,
				FBUsageAvgMiB:       1200,
				TensorPipeActiveMin: 0.01,
				TensorPipeActiveMax: 0.10,
				TensorPipeActiveAvg: 0.05,
				DRAMActiveMin:       0.02,
				DRAMActiveMax:       0.08,
				DRAMActiveAvg:       0.05,
				SMActiveMin:         smAvgs[i] - 0.03,
				SMActiveMax:         smAvgs[i] + 0.05,
				SMActiveAvg:         smAvgs[i],
			})
		}
	}
}

func gpuTimeslicingListBody(t *testing.T, app *echo.Echo, identity, query string) map[string]interface{} {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing"+query, nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func candidateNamespacesFromGPUList(body map[string]interface{}) map[string]struct{} {
	out := make(map[string]struct{})
	data, _ := body["data"].([]interface{})
	for _, raw := range data {
		rec, _ := raw.(map[string]interface{})
		cands, _ := rec["candidate_containers"].([]interface{})
		for _, cRaw := range cands {
			cand, _ := cRaw.(map[string]interface{})
			if ns, ok := cand["namespace"].(string); ok && ns != "" {
				out[ns] = struct{}{}
			}
		}
	}
	return out
}

func TestTagFilters_GPUTimeslicingList(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()
	seedTagFilterWorkloadKeys(t, ctx)

	prodWorkloads := [3]struct{ wl, cn string }{
		{"gpu-w1", "gpu-c1"},
		{"gpu-w2", "gpu-c2"},
		{"gpu-w3", "gpu-c3"},
	}
	stgWorkloads := [3]struct{ wl, cn string }{
		{"gpu-s1", "gpu-s1"},
		{"gpu-s2", "gpu-s2"},
		{"gpu-s3", "gpu-s3"},
	}

	for _, w := range prodWorkloads {
		_, err := database.Pool.Exec(ctx, `
			INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
			VALUES ($1, $2, $3, $4, 'Deployment', $5, '{"environment":"production"}'::jsonb)
			ON CONFLICT (org_id, namespace, workload, container_name)
			DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
			testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace, w.wl, w.cn)
		require.NoError(t, err)
	}
	for _, w := range stgWorkloads {
		_, err := database.Pool.Exec(ctx, `
			INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
			VALUES ($1, $2, 'other-ns', $3, 'Deployment', $4, '{"environment":"staging"}'::jsonb)
			ON CONFLICT (org_id, namespace, workload, container_name)
			DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
			testutil.TestOrgID, testutil.TestClusterUUID, w.wl, w.cn)
		require.NoError(t, err)
	}

	seedTagFilterGPUTimeslicingNode(t, database.Pool, testutil.TestNamespace, prodWorkloads, "gpu-node-prod")
	seedTagFilterGPUTimeslicingNode(t, database.Pool, "other-ns", stgWorkloads, "gpu-node-stg")

	unfilteredBody := gpuTimeslicingListBody(t, app, identity, "?limit=20")
	unfilteredData, _ := unfilteredBody["data"].([]interface{})
	require.NotEmpty(t, unfilteredData)

	unfilteredNS := candidateNamespacesFromGPUList(unfilteredBody)
	_, hasStagingNS := unfilteredNS["other-ns"]
	require.True(t, hasStagingNS, "unfiltered list must include staging-namespace GPU recs")

	filteredBody := gpuTimeslicingListBody(t, app, identity,
		"?filter%5Btag%3Aenvironment%5D=production&limit=20")
	filteredData, _ := filteredBody["data"].([]interface{})
	require.NotEmpty(t, filteredData)

	filteredNS := candidateNamespacesFromGPUList(filteredBody)
	_, stillHasStaging := filteredNS["other-ns"]
	assert.False(t, stillHasStaging, "production tag filter should exclude other-ns candidates")
	assert.Less(t, len(filteredData), len(unfilteredData),
		"tag filter should drop node recs whose candidates are only in non-matching namespaces")
}
