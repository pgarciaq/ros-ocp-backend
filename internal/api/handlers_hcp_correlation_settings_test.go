package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupHCPCorrelationSettingsTestEcho(t *testing.T, orgID string) *echo.Echo {
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
	v1.GET("/recommendations/openshift/settings/hcp-correlation", GetHCPCorrelationSettings)
	v1.PUT("/recommendations/openshift/settings/hcp-correlation", PutHCPCorrelationSettings)
	v1.DELETE("/recommendations/openshift/settings/hcp-correlation", DeleteHCPCorrelationSettings)
	return e
}

func TestGetHCPCorrelationSettings_ReturnsDefaults(t *testing.T) {
	orgID := "org-hcp-settings-api-get"
	e := setupHCPCorrelationSettingsTestEcho(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/hcp-correlation", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0.3, resp["h_p99_threshold_s"])
	assert.Equal(t, float64(1), resp["window_h"])
	assert.Equal(t, 100.0, resp["z_idle_req_per_day"])
	locked, ok := resp["locked_fields"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, locked)
}

func TestPutHCPCorrelationSettings_UpdatesAndReturns(t *testing.T) {
	orgID := "org-hcp-settings-api-put"
	e := setupHCPCorrelationSettingsTestEcho(t, orgID)

	body := `{"h_p99_threshold_s": 0.5, "h_baseline_multiple": 4, "c_cpu_pct": 85, "c_etcd_p99_s": 0.02, "window_h": 2, "skew_m": 10, "freshness_h": 4, "expiry_h": 48, "z_idle_req_per_day": 50, "z_idle_cpu_floor_mc": 25}`
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/hcp-correlation", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0.5, resp["h_p99_threshold_s"])
	assert.Equal(t, float64(2), resp["window_h"])
}

func TestPutHCPCorrelationSettings_RejectsInvalidValues(t *testing.T) {
	orgID := "org-hcp-settings-api-invalid"
	e := setupHCPCorrelationSettingsTestEcho(t, orgID)

	body := `{"h_p99_threshold_s": 0, "h_baseline_multiple": 4, "c_cpu_pct": 85, "c_etcd_p99_s": 0.02, "window_h": 2, "skew_m": 10, "freshness_h": 4, "expiry_h": 48, "z_idle_req_per_day": 50, "z_idle_cpu_floor_mc": 25}`
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/hcp-correlation", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "validation_errors")
}

func TestPutHCPCorrelationSettings_RejectsLockedField(t *testing.T) {
	orgID := "org-hcp-settings-api-locked"
	e := setupHCPCorrelationSettingsTestEcho(t, orgID)
	t.Setenv("ROS_HCP_C_CPU_PCT", "90")
	config.ResetForTest()
	_ = config.GetConfig()

	body := `{"h_p99_threshold_s": 0.5, "h_baseline_multiple": 4, "c_cpu_pct": 85, "c_etcd_p99_s": 0.02, "window_h": 2, "skew_m": 10, "freshness_h": 4, "expiry_h": 48, "z_idle_req_per_day": 50, "z_idle_cpu_floor_mc": 25}`
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/hcp-correlation", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "locked_fields")
}

func TestDeleteHCPCorrelationSettings_RestoresDefaults(t *testing.T) {
	orgID := "org-hcp-settings-api-delete"
	e := setupHCPCorrelationSettingsTestEcho(t, orgID)

	putBody := `{"h_p99_threshold_s": 0.5, "h_baseline_multiple": 4, "c_cpu_pct": 85, "c_etcd_p99_s": 0.02, "window_h": 2, "skew_m": 10, "freshness_h": 4, "expiry_h": 48, "z_idle_req_per_day": 50, "z_idle_cpu_floor_mc": 25}`
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/hcp-correlation", bytes.NewBufferString(putBody))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/hcp-correlation", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0.3, resp["h_p99_threshold_s"], "delete restores compiled defaults")
}
