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
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupVMRecommendationsHandler(t *testing.T, orgID string) *echo.Echo {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
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
	v1.GET("/recommendations/openshift/vm", GetVMRecommendations)
	v1.GET("/recommendations/openshift/vm/detail", GetVMRecommendationDetail)
	v1.GET("/recommendations/openshift/settings/vm", GetVMSettings)
	v1.GET("/recommendations/openshift/vm/detail", GetVMRecommendationDetail)
	return e
}

func TestVMRecommendations_ListEmpty(t *testing.T) {
	orgID := "org-vm-rec-empty-" + uuid.New().String()[:8]
	e := setupVMRecommendationsHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/vm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp VMRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count)
	assert.NotNil(t, resp.Data)
	assert.Empty(t, resp.Data)
}

func TestGetVMList_FilterTag(t *testing.T) {
	orgID := "org-vm-tag-" + uuid.New().String()[:8]
	clusterUUID := testutil.TestClusterUUID
	e := setupVMRecommendationsHandler(t, orgID)
	config.ResetTagsForTest()
	config.ResetTagsRuntimeForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "api")
	require.True(t, config.TagsFeatureEnabled())

	ctx := context.Background()

	_, err := database.Pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1::uuid, 'vm-tag-test', 1, NOW()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	_, err = database.Pool.Exec(ctx, `
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

	_, err = database.Pool.Exec(ctx, `
		INSERT INTO vm_recommendations (
			org_id, cluster_uuid, vm_name, namespace, guest_os,
			current_vcpu, current_memory_gib, recommended_vcpu, recommended_memory_gib,
			guest_agent_detected, confidence, term, engine,
			is_idle, is_abandoned, is_oversized, savings_amount, savings_currency, last_recommended_at
		) VALUES (
			$1, $2, 'vm-prod', $3, 'linux',
			4, 16, 2, 8,
			true, 'high', 'medium_term', 'cost',
			false, false, false, 10.00, 'USD', now()),
			($1, $2, 'vm-stg', 'other-ns', 'linux',
			4, 16, 2, 8,
			true, 'high', 'medium_term', 'cost',
			false, false, false, 20.00, 'USD', now())`,
		orgID, clusterUUID, testutil.TestNamespace)
	require.NoError(t, err)

	_, total, listErr := engine.ListVMRecommendations(ctx, database.Pool, orgID, engine.VMRecommendationFilters{
		ClusterUUIDs: []string{clusterUUID},
		TagFilters:   []model.TagFilter{{Key: "environment", Values: []string{"production"}}},
	})
	require.NoError(t, listErr)
	require.Equal(t, int64(1), total, "engine tag filter should return one VM")

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm?tag=environment:production", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp VMRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, testutil.TestNamespace, resp.Data[0].Namespace)
}

func TestVMRecommendations_ListAcceptsFilters(t *testing.T) {
	orgID := "org-vm-rec-filters-" + uuid.New().String()[:8]
	e := setupVMRecommendationsHandler(t, orgID)

	url := "/api/cost-management/v1/recommendations/openshift/vm" +
		"?filter[namespace]=prod&filter[vm_name]=web&filter[cluster]=00000000-0000-0000-0000-000000000099" +
		"&filter[term]=short_term&filter[engine]=cost&filter[is_idle]=true&limit=5&offset=0"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

func TestVMRecommendations_ListInvalidLimit_Returns400(t *testing.T) {
	orgID := "org-vm-rec-bad-limit-" + uuid.New().String()[:8]
	e := setupVMRecommendationsHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/vm?limit=500", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestVMRecSavingsObject(t *testing.T) {
	assert.Nil(t, vmRecSavingsObject(nil, nil))

	amt := 123.45
	cur := "EUR"
	got := vmRecSavingsObject(&amt, &cur)
	require.NotNil(t, got)
	assert.Equal(t, "EUR", got.Units)
	assert.Equal(t, "123.450000", got.Value)
}

func TestParseVMRecBoolFilter_InvalidValue(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?filter%5Bguest_agent_detected%5D=maybe", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_, err := parseVMRecBoolFilter(c, "guest_agent_detected")
	require.Error(t, err)
}

func TestVMRecommendations_ListInvalidGuestAgentFilter_Returns400(t *testing.T) {
	orgID := "org-vm-rec-bad-ga-" + uuid.New().String()[:8]
	e := setupVMRecommendationsHandler(t, orgID)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm?filter%5Bguest_agent_detected%5D=maybe",
		nil,
	)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestVMRecommendations_DetailMissingParams(t *testing.T) {
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("Identity", identity.XRHID{
				Identity: identity.Identity{OrgID: "org-vm-detail"},
			})
			return next(c)
		}
	})
	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/vm/detail", GetVMRecommendationDetail)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/vm/detail", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "error", body["status"])
}

func TestVMSettings_GET_ReturnsConfig(t *testing.T) {
	orgID := "org-vm-settings-get-" + uuid.New().String()[:8]
	e := setupVMRecommendationsHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/vm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp, "thresholds")
	assert.Contains(t, resp, "memory_floors")
	assert.Contains(t, resp, "disk")
	assert.Contains(t, resp, "io")
	assert.Contains(t, resp, "placement")
	placement, ok := resp["placement"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, placement["enable_placement_checks"])
	assert.Equal(t, float64(3), placement["placement_skew_ratio"])
	assert.Equal(t, true, placement["enable_shared_pvc_correlation"])
	assert.Equal(t, float64(64), placement["numa_node_memory_gib"])
}

func TestVMRecMetadata_JSONIncludesPlacementFields(t *testing.T) {
	meta := vmRecMetadata{
		IsRedundantPlacement: true,
		HasSharedStorage:     true,
		NUMAOversized:        true,
	}
	b, err := json.Marshal(meta)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, true, decoded["is_redundant_placement"])
	assert.Equal(t, true, decoded["has_shared_storage"])
	assert.Equal(t, true, decoded["numa_oversized"])
}

func TestParseVMNotifications_StructuredObjects(t *testing.T) {
	raw := []byte(`[
		{"code":19,"type":"warning","message":"VM is oversized"},
		{"code":38,"type":"info","message":"QEMU guest agent not installed"}
	]`)
	out := parseVMNotifications(raw)
	require.Len(t, out, 2)

	first, ok := out[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(19), first["code"])
	assert.Equal(t, "warning", first["type"])

	second, ok := out[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(38), second["code"])
}

func TestParseVMNotifications_LegacyIntArray(t *testing.T) {
	raw := []byte(`[18,19]`)
	out := parseVMNotifications(raw)
	require.Len(t, out, 2)
	assert.Equal(t, float64(18), out[0])
	assert.Equal(t, float64(19), out[1])
}

func TestVMRecAllowedOrderBy_MatchesDBColumns(t *testing.T) {
	for apiKey, dbCol := range vmRecAllowedOrderBy {
		assert.Equal(t, apiKey, dbCol, "API key should match DB column for %q", apiKey)
	}
	expected := []string{
		"vm_name", "namespace", "current_vcpu", "current_memory_gib", "guest_os",
		"recommended_vcpu", "recommended_memory_gib", "is_idle", "is_abandoned", "is_oversized",
		"confidence", "last_recommended_at",
	}
	for _, key := range expected {
		_, ok := vmRecAllowedOrderBy[key]
		assert.True(t, ok, "missing order_by key %q", key)
	}
}
