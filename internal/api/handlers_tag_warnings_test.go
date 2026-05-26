package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestTagWarnings_UnknownKeyEmptyResults(t *testing.T) {
	withTagsEnabled(t)

	app, identity, _, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	url := "/api/cost-management/v1/recommendations/openshift?limit=50&filter%5Btag%3Acost_center%5D=production"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	meta, _ := body["meta"].(map[string]interface{})
	require.NotNil(t, meta)
	data, _ := body["data"].([]interface{})
	assert.Empty(t, data)
	warnings, _ := meta["warnings"].([]interface{})
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0].(string), "cost_center")
}

func TestTagWarnings_NoWarningWhenResultsNonEmpty(t *testing.T) {
	withTagsEnabled(t)

	app, identity, _, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?limit=50&filter%5Btag%3Aenvironment%5D=production", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	meta, _ := body["meta"].(map[string]interface{})
	require.NotNil(t, meta)
	_, hasWarnings := meta["warnings"]
	assert.False(t, hasWarnings)
}

func TestTagWarnings_IgnoredWhenTagsDisabled(t *testing.T) {
	withTagsDisabled(t)

	app, identity, _, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?limit=50&filter%5Btag%3Acost_center%5D=production", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	meta, _ := body["meta"].(map[string]interface{})
	require.NotNil(t, meta)
	_, hasWarnings := meta["warnings"]
	assert.False(t, hasWarnings)
}

func TestTagWarnings_SyncNotRunAPIMode(t *testing.T) {
	withTagsEnabled(t)
	require.True(t, config.TagsUsePushSync())

	app, identity, _, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?limit=50&filter%5Btag%3Aenvironment%5D=nonexistent-value", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	meta, _ := body["meta"].(map[string]interface{})
	require.NotNil(t, meta)
	data, _ := body["data"].([]interface{})
	assert.Empty(t, data)
	warnings, _ := meta["warnings"].([]interface{})
	require.NotEmpty(t, warnings)
	var combined string
	for _, w := range warnings {
		combined += w.(string) + " "
	}
	assert.True(t,
		strings.Contains(combined, "push sync") || strings.Contains(combined, "tag catalog"),
		"warnings: %v", warnings,
	)
}
