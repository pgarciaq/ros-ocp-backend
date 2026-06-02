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
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupQuotaSettingsTestEcho(t *testing.T, orgID string) *echo.Echo {
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
	v1.GET("/recommendations/openshift/settings/quota", GetQuotaSettings)
	v1.PUT("/recommendations/openshift/settings/quota", PutQuotaSettings)
	v1.DELETE("/recommendations/openshift/settings/quota", DeleteQuotaSettings)
	return e
}

func TestGetQuotaSettings_ReturnsDefaults(t *testing.T) {
	orgID := "org-quota-settings-api-get"
	e := setupQuotaSettingsTestEcho(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/quota", nil)
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

func TestPutQuotaSettings_TriggersAsyncRecalculation(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_THRESHOLD_RECALCULATION_ENABLED", "true")

	orgID := "org-quota-settings-api-recalc"
	e := setupQuotaSettingsTestEcho(t, orgID)

	var triggeredOrg, triggeredType string
	engine.SetThresholdRecalcHookForTest(func(oid, rt string) {
		triggeredOrg = oid
		triggeredType = rt
	})
	defer engine.ClearThresholdRecalcHookForTest()

	body := bytes.NewReader([]byte(`{
		"headroom_percent": 15,
		"high_risk_threshold_percent": 85,
		"medium_risk_threshold_percent": 65
	}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/quota", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, orgID, triggeredOrg)
	assert.Equal(t, "quota", triggeredType)
}

func TestPutQuotaSettings_UpdatesAndReturns(t *testing.T) {
	orgID := "org-quota-settings-api-put"
	e := setupQuotaSettingsTestEcho(t, orgID)

	body := bytes.NewReader([]byte(`{
		"headroom_percent": 15,
		"high_risk_threshold_percent": 85,
		"medium_risk_threshold_percent": 65
	}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/quota", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, float64(15), resp["headroom_percent"])
	assert.Equal(t, float64(85), resp["high_risk_threshold_percent"])
	assert.Equal(t, float64(65), resp["medium_risk_threshold_percent"])

	req2 := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/quota", nil)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	var resp2 map[string]interface{}
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Equal(t, float64(15), resp2["headroom_percent"])
}

func TestPutQuotaSettings_RejectsInvalidValues(t *testing.T) {
	orgID := "org-quota-settings-api-bad"
	e := setupQuotaSettingsTestEcho(t, orgID)

	cases := []struct {
		name string
		body string
	}{
		{"headroom over 100", `{"headroom_percent": 150, "high_risk_threshold_percent": 90, "medium_risk_threshold_percent": 70}`},
		{"medium >= high", `{"headroom_percent": 10, "high_risk_threshold_percent": 70, "medium_risk_threshold_percent": 80}`},
		{"high at 0", `{"headroom_percent": 10, "high_risk_threshold_percent": 0, "medium_risk_threshold_percent": 50}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/quota",
				bytes.NewReader([]byte(tc.body)))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			require.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestDeleteQuotaSettings_RestoresDefaults(t *testing.T) {
	orgID := "org-quota-settings-api-delete"
	e := setupQuotaSettingsTestEcho(t, orgID)

	putBody := bytes.NewReader([]byte(`{
		"headroom_percent": 20,
		"high_risk_threshold_percent": 80,
		"medium_risk_threshold_percent": 55
	}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/quota", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/quota", nil)
	delRec := httptest.NewRecorder()
	e.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusOK, delRec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(delRec.Body.Bytes(), &resp))
	assert.Equal(t, float64(10), resp["headroom_percent"])
	assert.Equal(t, float64(90), resp["high_risk_threshold_percent"])
	assert.Equal(t, float64(70), resp["medium_risk_threshold_percent"])
}

func TestRegisterDisabledPluginRouteGuards_QuotaSettingsDisabled_Returns404(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()
	_ = config.GetConfig()

	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	registerDisabledPluginRouteGuards(v1)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/quota", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "not_found", body["status"])
	msg, ok := body["message"].(string)
	require.True(t, ok)
	require.Contains(t, msg, "plugin 'quota' is not enabled")
}
