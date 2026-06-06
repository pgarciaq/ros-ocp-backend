package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
