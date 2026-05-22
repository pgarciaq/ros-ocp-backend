package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRegisterDisabledPluginRouteGuards_GPUDisabled_Returns404(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "container")

	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	registerDisabledPluginRouteGuards(v1)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "not_found", body["status"])
	msg, ok := body["message"].(string)
	require.True(t, ok)
	require.Contains(t, msg, "plugin 'gpu' is not enabled")
}

func TestBusinessHoursDisabled_Routes404(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "false")
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	config.ResetForTest()
	_ = config.GetConfig()

	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	registerBusinessHoursRouteGuards(v1)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/business-hours"},
		{http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours"},
		{http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/business-hours"},
		{http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/business-hours/clusters/550e8400-e29b-41d4-a716-446655440000"},
		{http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours/clusters/550e8400-e29b-41d4-a716-446655440000"},
		{http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/business-hours/clusters/550e8400-e29b-41d4-a716-446655440000"},
		{http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/business-hours/clusters/550e8400-e29b-41d4-a716-446655440000/namespaces/openshift-console"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			require.Equal(t, http.StatusNotFound, rec.Code, "body: %s", rec.Body.String())

			var body map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, "not_found", body["status"])
			msg, ok := body["message"].(string)
			require.True(t, ok)
			require.Contains(t, msg, "plugin 'business-hours' is not enabled")
		})
	}
}

func TestBusinessHoursEnabled_RouteGuardsNotRegistered(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	config.ResetForTest()
	_ = config.GetConfig()

	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	registerBusinessHoursRouteGuards(v1)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	// No guard handler — Echo default 404, not plugin disabled JSON.
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotEqual(t, "not_found", body["status"])
}
