package model_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func assertAllNativeRecKeysAllowed(t *testing.T, keys map[string]interface{}) {
	t.Helper()
	for k := range keys {
		require.True(t, model.IsAllowedNativeRecommendationQueryKey(k),
			"recommendation ApplyQueryParams key %q must be allowlisted (internal/model/native_query_allowlist.go)", k)
	}
}

func assertAllNativeNSKeysAllowed(t *testing.T, keys map[string]interface{}) {
	t.Helper()
	for k := range keys {
		require.True(t, model.IsAllowedNativeNamespaceQueryKey(k),
			"namespace applyNSQueryParams key %q must be allowlisted (internal/model/native_query_allowlist.go)", k)
	}
}

func echoCtxGET(query url.Values) echo.Context {
	e := echo.New()
	target := "/"
	if enc := query.Encode(); enc != "" {
		target += "?" + enc
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec)
}

// Exercise MapNativeQueryParameters keys (handlers.go) against the allowlist, including
// multi-value OR composites from buildNativeModeClause and exclude+exact pairs.
func TestNativeQueryAllowlist_MapNativeQueryParameters(t *testing.T) {
	u := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	query := url.Values{
		"cluster":                 {u.String(), "prod-alias"},
		"project":                 {"ns-alpha", "ns-beta"},
		"workload":                {"cart-svc", "pay-svc"},
		"workload_type":           {"deployment"},
		"container":               {"web", "api"},
		"exclude[project]":        {"kube-system"},
		"filter[exact:container]": {"exact-c"},
		"filter[exact:workload]":  {"wl-ex"},
		"exclude[workload_type]":  {"daemonset"},
		"stale":                   {"only"},
		"start_date":              {"2025-01-15"},
		"end_date":                {"2025-01-20"},
	}
	c := echoCtxGET(query)
	params, err := api.MapNativeQueryParameters(c)
	require.NoError(t, err)
	assertAllNativeRecKeysAllowed(t, params)

	// Wrapped-parens composite (single-column OR) still resolves to allowlisted atoms.
	require.True(t, model.IsAllowedNativeRecommendationQueryKey(
		"(rs.namespace ILIKE ? OR rs.namespace ILIKE ? OR rs.namespace ILIKE ?)"))
	require.True(t, model.IsAllowedNativeRecommendationQueryKey(
		"(rs.workload ILIKE ? AND rs.workload ILIKE ?)"))
	require.True(t, model.IsAllowedNativeRecommendationQueryKey(
		"(rs.namespace ILIKE ? OR rs.workload ILIKE ?)"))
}

func TestNativeQueryAllowlist_MapNativeNamespaceQueryParameters(t *testing.T) {
	clusterUUID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	query := url.Values{
		"cluster":               {"edge-cluster", clusterUUID.String()},
		"project":               {"team-a", "team-b"},
		"exclude[project]":      {"kube-public"},
		"filter[exact:project]": {"exact-ns"},
		"start_date":            {"2025-02-01"},
		"end_date":              {"2025-02-10"},
	}
	c := echoCtxGET(query)
	params, err := api.MapNativeNamespaceQueryParameters(c)
	require.NoError(t, err)
	assertAllNativeNSKeysAllowed(t, params)

	require.True(t, model.IsAllowedNativeNamespaceQueryKey(
		"(ns.namespace_name ILIKE ? OR ns.namespace_name ILIKE ?)"))
	require.True(t, model.IsAllowedNativeNamespaceQueryKey(
		"(c.cluster_alias ILIKE ? OR c.cluster_uuid = ?)"))
}

func TestNativeQueryAllowlist_MapQualityQueryParameters(t *testing.T) {
	query := url.Values{
		"start_date": {"2025-03-01"},
		"end_date":   {"2025-03-05"},
		"cluster":    {"c1", "c2"},
		"project":    {"p1"},
		"workload":   {"w1"},
		"container":  {"ctr1"},
	}
	c := echoCtxGET(query)
	params, err := api.MapQualityQueryParameters(c)
	require.NoError(t, err)
	assertAllNativeRecKeysAllowed(t, params)
}

func TestNativeQueryAllowlist_MapHistoryQueryParameters(t *testing.T) {
	query := url.Values{
		"start_date": {"2025-04-01"},
		"end_date":   {"2025-04-07"},
		"cluster":    {"hist-cluster"},
		"project":    {"hp"},
		"workload":   {"hw"},
		"container":  {"hc"},
		"term":       {"short", "medium"},
		"engine":     {"cost"},
	}
	c := echoCtxGET(query)
	params, err := api.MapHistoryQueryParameters(c)
	require.NoError(t, err)
	assertAllNativeRecKeysAllowed(t, params)
}

func TestNativeQueryAllowlist_GPUFilters(t *testing.T) {
	query := url.Values{
		"start_date":         {"2025-01-15"},
		"gpu_model":          {"A100", "H100"},
		"gpu_classification": {"idle", "underutilized"},
		"has_gpu":            {"true"},
	}
	c := echoCtxGET(query)
	params, err := api.MapNativeQueryParameters(c)
	require.NoError(t, err)
	assertAllNativeRecKeysAllowed(t, params)

	require.True(t, model.IsAllowedNativeRecommendationQueryKey("rs.gpu_model_name ILIKE ?"))
	require.True(t, model.IsAllowedNativeRecommendationQueryKey("rs.gpu_model_name ILIKE ? OR rs.gpu_model_name ILIKE ?"))
	require.True(t, model.IsAllowedNativeRecommendationQueryKey("rs.gpu_classification IN ?"))
	require.True(t, model.IsAllowedNativeRecommendationQueryKey("rs.has_gpu = ?"))
}

func TestNativeQueryAllowlist_RejectsUnknownKey(t *testing.T) {
	require.False(t, model.IsAllowedNativeRecommendationQueryKey("rs.evil = ?"))
	require.False(t, model.IsAllowedNativeNamespaceQueryKey("ns.evil = ?"))
}
