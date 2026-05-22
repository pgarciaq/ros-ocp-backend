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

func TestCapabilities_BusinessHoursFalse(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "false")
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	config.ResetForTest()
	_ = config.GetConfig()

	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/settings/capabilities", GetCapabilities)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/capabilities", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp capabilitiesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.False(t, resp.BusinessHours)
}

func TestCapabilities_BusinessHoursTrue(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	config.ResetForTest()
	_ = config.GetConfig()

	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/settings/capabilities", GetCapabilities)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/capabilities", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp capabilitiesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.BusinessHours)
}
