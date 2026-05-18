package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
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
