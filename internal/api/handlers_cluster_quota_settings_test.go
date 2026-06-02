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

func setupClusterQuotaSettingsTestEcho(t *testing.T, orgID string) *echo.Echo {
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
	v1.GET("/recommendations/openshift/settings/cluster-quota", GetClusterQuotaSettings)
	v1.PUT("/recommendations/openshift/settings/cluster-quota", PutClusterQuotaSettings)
	v1.DELETE("/recommendations/openshift/settings/cluster-quota", DeleteClusterQuotaSettings)
	return e
}

func TestGetClusterQuotaSettings_ReturnsDefaults(t *testing.T) {
	orgID := "org-cluster-quota-settings-api-get"
	e := setupClusterQuotaSettingsTestEcho(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/cluster-quota", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, float64(10), resp["headroom_percent"])
	assert.Equal(t, float64(90), resp["high_risk_threshold_percent"])
	assert.Equal(t, float64(70), resp["medium_risk_threshold_percent"])
	locked, ok := resp["locked_fields"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, locked)
}

func TestPutClusterQuotaSettings_RejectsLockedEnvField(t *testing.T) {
	t.Setenv("ROS_CLUSTER_QUOTA_HEADROOM_PERCENT", "12")
	orgID := "org-cluster-quota-settings-api-locked"
	e := setupClusterQuotaSettingsTestEcho(t, orgID)

	body := bytes.NewReader([]byte(`{
		"headroom_percent": 20,
		"high_risk_threshold_percent": 90,
		"medium_risk_threshold_percent": 70
	}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/cluster-quota", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRegisterDisabledPluginRouteGuards_ClusterQuotaDisabled_Returns404(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()
	_ = config.GetConfig()

	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	registerDisabledPluginRouteGuards(v1)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/cluster-quota", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "not_found", body["status"])
	msg, ok := body["message"].(string)
	require.True(t, ok)
	require.Contains(t, msg, "plugin 'cluster-quota' is not enabled")
}

func TestPutClusterQuotaSettings_UpdatesAndReturns(t *testing.T) {
	orgID := "org-cluster-quota-settings-api-put"
	e := setupClusterQuotaSettingsTestEcho(t, orgID)

	body := bytes.NewReader([]byte(`{
		"headroom_percent": 15,
		"high_risk_threshold_percent": 85,
		"medium_risk_threshold_percent": 65
	}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/cluster-quota", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, float64(15), resp["headroom_percent"])
	assert.Equal(t, float64(85), resp["high_risk_threshold_percent"])
	assert.Equal(t, float64(65), resp["medium_risk_threshold_percent"])

	req2 := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/cluster-quota", nil)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	var resp2 map[string]interface{}
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Equal(t, float64(15), resp2["headroom_percent"])
}

func TestDeleteClusterQuotaSettings_RestoresDefaults(t *testing.T) {
	orgID := "org-cluster-quota-settings-api-delete"
	e := setupClusterQuotaSettingsTestEcho(t, orgID)

	putBody := bytes.NewReader([]byte(`{
		"headroom_percent": 20,
		"high_risk_threshold_percent": 80,
		"medium_risk_threshold_percent": 55
	}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/cluster-quota", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/cluster-quota", nil)
	delRec := httptest.NewRecorder()
	e.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusOK, delRec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(delRec.Body.Bytes(), &resp))
	assert.Equal(t, float64(10), resp["headroom_percent"])
	assert.Equal(t, float64(90), resp["high_risk_threshold_percent"])
	assert.Equal(t, float64(70), resp["medium_risk_threshold_percent"])
}
