package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestFleetSavingsSummary_GroupByTag_BracketSyntax(t *testing.T) {
	withTagsEnabled(t)

	app, identity, _, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/savings-summary?group_by%5Btag%3Aenvironment%5D=*", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		Data []struct {
			TagValue                *string `json:"tag_value"`
			EstimatedMonthlySavings struct {
				Value string `json:"value"`
			} `json:"estimated_monthly_savings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.GreaterOrEqual(t, body.Meta.Count, 1)
	require.NotEmpty(t, body.Data)

	var prodFound bool
	for _, row := range body.Data {
		if row.TagValue != nil && *row.TagValue == "production" {
			prodFound = true
			assert.Equal(t, "100.00", row.EstimatedMonthlySavings.Value)
		}
	}
	assert.True(t, prodFound, "expected production tag group with container savings")
}

func TestFleetSavingsSummary_GroupByTag_UnknownKeyEmptyGroups(t *testing.T) {
	withTagsEnabled(t)

	app, identity, _, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/savings-summary?group_by%5Btag%3Acost_center%5D=*", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		Data []interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 1, body.Meta.Count)
	require.Len(t, body.Data, 1)
}

func TestFleetSavingsSummary_GroupByTag_IgnoredWhenDisabled(t *testing.T) {
	withTagsDisabled(t)

	app, identity, _, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/savings-summary?group_by%5Btag%3Aenvironment%5D=*", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	_, hasByCluster := body["by_cluster"]
	assert.True(t, hasByCluster, "disabled group_by should return default fleet summary shape")
}

func TestFleetSavingsSummary_GroupByTag_ClusterAndNamespaceFilters(t *testing.T) {
	withTagsEnabled(t)

	app, identity, _, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/savings-summary?group_by%5Btag%3Aenvironment%5D=*&filter%5Bcluster%5D="+testutil.TestClusterUUID+
			"&filter%5Bproject%5D="+testutil.TestNamespace, nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		Data []struct {
			TagValue                *string `json:"tag_value"`
			EstimatedMonthlySavings struct {
				Value string `json:"value"`
			} `json:"estimated_monthly_savings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.NotNil(t, body.Data[0].TagValue)
	assert.Equal(t, "production", *body.Data[0].TagValue)
	assert.Equal(t, "100.00", body.Data[0].EstimatedMonthlySavings.Value)
}

func TestFleetSavingsSummary_GroupByTag_FlatSyntax(t *testing.T) {
	withTagsEnabled(t)

	app, identity, _, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/savings-summary?group_by=tag:environment", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Data []interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotEmpty(t, body.Data)
}

// TestFleetSavingsSummary_GroupByTag_MultiValueExplodesGroups pins the #814
// explosion: a container whose tag holds multiple values contributes its
// savings to EACH value's group (a [production,staging] container with $50
// adds $50 to both groups), a scalar legacy row contributes to its one
// group, and the missing-key NULL group is preserved.
func TestFleetSavingsSummary_GroupByTag_MultiValueExplodesGroups(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	for _, ns := range []struct {
		name  string
		tags  string
		cents int
	}{
		{"multi-ns", `{"environment":["production","staging"]}`, 5000},
		{"no-key-ns", `{"team":"platform"}`, 7000},
	} {
		_, err := database.Pool.Exec(ctx, `
			INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
			VALUES ($1, $2, $3, 'w-`+ns.name+`', 'Deployment', 'c-`+ns.name+`', $4::jsonb)
			ON CONFLICT (org_id, namespace, workload, container_name)
			DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
			testutil.TestOrgID, testutil.TestClusterUUID, ns.name, ns.tags)
		require.NoError(t, err)
		_, err = database.Pool.Exec(ctx, `
			INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_savings_cents, updated_at)
			VALUES ($1, $2, $3, 'w-`+ns.name+`', 'Deployment', 'c-`+ns.name+`', 'medium', 'cost', false, '{}', $4, now())
			ON CONFLICT DO NOTHING`,
			testutil.TestOrgID, testutil.TestClusterUUID, ns.name, ns.cents)
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/savings-summary?group_by%5Btag%3Aenvironment%5D=*", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Data []struct {
			TagValue                *string `json:"tag_value"`
			EstimatedMonthlySavings struct {
				Value string `json:"value"`
			} `json:"estimated_monthly_savings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	groups := make(map[string]string)
	var nullGroup string
	for _, row := range body.Data {
		if row.TagValue == nil {
			nullGroup = row.EstimatedMonthlySavings.Value
			continue
		}
		groups[*row.TagValue] = row.EstimatedMonthlySavings.Value
	}
	// Base fixture: production $100 (TestNamespace), staging $200 (other-ns);
	// the multi-ns container adds $50 to BOTH groups (the explosion).
	assert.Equal(t, "150.00", groups["production"],
		"production = scalar $100 + the multi-valued container's $50")
	assert.Equal(t, "250.00", groups["staging"],
		"staging = scalar $200 + the multi-valued container's $50")
	assert.Equal(t, "70.00", nullGroup,
		"containers without the key keep their savings in the NULL group")
}
