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
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
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
			category, estimated_savings_cents, savings_currency, last_recommended_at
		) VALUES (
			$1, $2, 'vm-prod', $3, 'linux',
			4, 16, 2, 8,
			true, 'high', 'medium_term', 'cost',
			'optimized', 10.00, 'USD', now()),
			($1, $2, 'vm-stg', 'other-ns', 'linux',
			4, 16, 2, 8,
			true, 'high', 'medium_term', 'cost',
			'optimized', 20.00, 'USD', now())`,
		orgID, clusterUUID, testutil.TestNamespace)
	require.NoError(t, err)

	_, total, listErr := vm.ListVMRecommendations(ctx, database.Pool, orgID, vm.VMRecommendationFilters{
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
		"&filter[term]=short_term&filter[engine]=cost&filter[category]=idle&limit=5&offset=0"
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

func TestVMRecMoneyAmount(t *testing.T) {
	assert.Nil(t, vmRecMoneyAmount(nil, nil))

	cents := int64(12345)
	cur := "EUR"
	got := vmRecMoneyAmount(&cents, &cur)
	require.NotNil(t, got)
	assert.Equal(t, "EUR", got.Units)
	assert.Equal(t, "123.45", got.Value)
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

func TestVMRecommendations_ListFilterProjectAliasMatchesNamespace(t *testing.T) {
	orgID := "org-vm-project-alias-" + uuid.New().String()[:8]
	clusterUUID := testutil.TestClusterUUID
	e := setupVMRecommendationsHandler(t, orgID)
	ctx := context.Background()

	_, err := database.Pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1::uuid, 'vm-proj-alias', 1, NOW()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	targetNS := "payments-ns"
	otherNS := "other-ns"
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO vm_recommendations (
			org_id, cluster_uuid, vm_name, namespace, guest_os,
			current_vcpu, current_memory_gib, recommended_vcpu, recommended_memory_gib,
			guest_agent_detected, confidence, term, engine,
			category, last_recommended_at
		) VALUES
			($1, $2, 'vm-pay', $3, 'linux', 4, 16, 2, 8, true, 'high', 'medium_term', 'cost', 'optimized', now()),
			($1, $2, 'vm-other', $4, 'linux', 4, 16, 2, 8, true, 'high', 'medium_term', 'cost', 'optimized', now())`,
		orgID, clusterUUID, targetNS, otherNS)
	require.NoError(t, err)

	for _, filter := range []string{
		"filter%5Bnamespace%5D=" + targetNS,
		"filter%5Bproject%5D=" + targetNS,
	} {
		t.Run(filter, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet,
				"/api/cost-management/v1/recommendations/openshift/vm?"+filter, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var resp VMRecommendationListResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			require.Equal(t, 1, resp.Meta.Count)
			require.Len(t, resp.Data, 1)
			assert.Equal(t, targetNS, resp.Data[0].Namespace)
			assert.Equal(t, "vm-pay", resp.Data[0].VMName)
		})
	}
}

func TestVMRecommendations_ListOrderBySavings(t *testing.T) {
	orgID := "org-vm-order-savings-" + uuid.New().String()[:8]
	clusterUUID := testutil.TestClusterUUID
	e := setupVMRecommendationsHandler(t, orgID)
	ctx := context.Background()

	_, err := database.Pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1::uuid, 'vm-savings-sort', 1, NOW()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	_, err = database.Pool.Exec(ctx, `
		INSERT INTO vm_recommendations (
			org_id, cluster_uuid, vm_name, namespace, guest_os,
			current_vcpu, current_memory_gib, recommended_vcpu, recommended_memory_gib,
			guest_agent_detected, confidence, term, engine,
			category, estimated_savings_cents, savings_currency, last_recommended_at
		) VALUES
			($1, $2, 'vm-low', 'ns', 'linux', 4, 16, 2, 8, true, 'high', 'medium_term', 'cost', 'optimized', 5.00, 'USD', now()),
			($1, $2, 'vm-high', 'ns', 'linux', 4, 16, 2, 8, true, 'high', 'medium_term', 'cost', 'optimized', 50.00, 'USD', now())`,
		orgID, clusterUUID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm?order_by=estimated_monthly_savings&order_how=desc", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp VMRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 2)
	require.NotNil(t, resp.Data[0].Savings)
	require.NotNil(t, resp.Data[1].Savings)
	assert.Equal(t, "vm-high", resp.Data[0].VMName)
	assert.Equal(t, "vm-low", resp.Data[1].VMName)
}

func TestVMRecommendations_ListCSVExport(t *testing.T) {
	orgID := "org-vm-csv-" + uuid.New().String()[:8]
	clusterUUID := testutil.TestClusterUUID
	e := setupVMRecommendationsHandler(t, orgID)
	ctx := context.Background()

	_, err := database.Pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1::uuid, 'vm-csv', 1, NOW()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO vm_recommendations (
			org_id, cluster_uuid, vm_name, namespace, guest_os,
			current_vcpu, current_memory_gib, recommended_vcpu, recommended_memory_gib,
			guest_agent_detected, confidence, term, engine,
			category, estimated_savings_cents, savings_currency, last_recommended_at
		) VALUES ($1, $2, 'csv-vm', 'csv-ns', 'linux', 4, 16, 2, 8, true, 'high', 'medium_term', 'cost', 'oversized', 12.50, 'USD', now())`,
		orgID, clusterUUID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm?format=csv", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/csv")
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	body := rec.Body.String()
	assert.Contains(t, body, "vm_name")
	assert.Contains(t, body, "csv-vm")
}

func TestVMRecAllowedOrderBy_MatchesDBColumns(t *testing.T) {
	for apiKey, dbCol := range vmRecAllowedOrderBy {
		if apiKey == "estimated_monthly_savings" || apiKey == "savings" || apiKey == "savings_amount" {
			assert.Equal(t, "estimated_savings_cents", dbCol)
			continue
		}
		assert.Equal(t, apiKey, dbCol, "API key should match DB column for %q", apiKey)
	}
	expected := []string{
		"vm_name", "namespace", "current_vcpu", "current_memory_gib", "guest_os",
		"recommended_vcpu", "recommended_memory_gib", "category",
		"confidence", "last_recommended_at", "estimated_monthly_savings", "savings", "savings_amount",
	}
	for _, key := range expected {
		_, ok := vmRecAllowedOrderBy[key]
		assert.True(t, ok, "missing order_by key %q", key)
	}
}
