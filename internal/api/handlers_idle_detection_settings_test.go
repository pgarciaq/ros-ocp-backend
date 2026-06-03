package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupIdleDetectionSettingsTestEcho(t *testing.T, orgID string) *echo.Echo {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	db.Pool = pool
	t.Cleanup(func() { db.Pool = nil })

	config.ResetForTest()
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
	v1.GET("/recommendations/openshift/settings/idle-detection", GetIdleDetectionSettings)
	v1.PUT("/recommendations/openshift/settings/idle-detection", PutIdleDetectionSettings)
	v1.DELETE("/recommendations/openshift/settings/idle-detection", DeleteIdleDetectionSettings)
	return e
}

func TestGetIdleDetectionSettings_ReturnsDefaults(t *testing.T) {
	orgID := "org-idle-settings-get-" + uuid.New().String()[:8]
	e := setupIdleDetectionSettingsTestEcho(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/idle-detection", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	idle, ok := resp["idle_detection"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, idle["enabled"])
	thresholds, ok := idle["thresholds"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(2), thresholds["cpu_utilization_percent"])
}

func TestPutIdleDetectionSettings_UpdatesSettings(t *testing.T) {
	orgID := "org-idle-settings-put-" + uuid.New().String()[:8]
	e := setupIdleDetectionSettingsTestEcho(t, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	body := bytes.NewReader([]byte(`{"idle_detection": {"enabled": false}}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/idle-detection", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	idle := resp["idle_detection"].(map[string]any)
	assert.Equal(t, false, idle["enabled"])
}

func TestPutIdleDetectionSettings_TriggersAsyncRecalculation(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_THRESHOLD_RECALCULATION_ENABLED", "true")

	orgID := "org-idle-settings-recalc-" + uuid.New().String()[:8]
	e := setupIdleDetectionSettingsTestEcho(t, orgID)

	var mu sync.Mutex
	var triggeredOrg string
	var triggeredTypes []string
	engine.SetThresholdRecalcHookForTest(func(oid, rt string) {
		mu.Lock()
		triggeredOrg = oid
		triggeredTypes = append(triggeredTypes, rt)
		mu.Unlock()
	})
	defer engine.ClearThresholdRecalcHookForTest()

	body := bytes.NewReader([]byte(`{"idle_detection": {"thresholds": {"cpu_utilization_percent": 5}}}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/idle-detection", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, orgID, triggeredOrg)
	assert.ElementsMatch(t, []string{"container", "gpu", "namespace", "node", "pvc"}, triggeredTypes)
}

func TestPutIdleDetectionSettings_ForbiddenWhenEnvLocksField(t *testing.T) {
	t.Setenv("ROS_IDLE_DETECTION_ENABLED", "true")
	orgID := "org-idle-settings-locked-" + uuid.New().String()[:8]
	e := setupIdleDetectionSettingsTestEcho(t, orgID)

	body := bytes.NewReader([]byte(`{"idle_detection": {"enabled": false}}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/idle-detection", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestPutIdleDetectionSettings_RejectsInvalidValues(t *testing.T) {
	orgID := "org-idle-settings-invalid-" + uuid.New().String()[:8]
	e := setupIdleDetectionSettingsTestEcho(t, orgID)

	body := bytes.NewReader([]byte(`{"idle_detection": {"thresholds": {"cpu_utilization_percent": 99}}}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/idle-detection", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "error", resp["status"])
	errs, ok := resp["validation_errors"].([]any)
	require.True(t, ok)
	assert.NotEmpty(t, errs)
}

func TestDeleteIdleDetectionSettings_ResetsToDefaults(t *testing.T) {
	orgID := "org-idle-settings-delete-" + uuid.New().String()[:8]
	e := setupIdleDetectionSettingsTestEcho(t, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	putBody := bytes.NewReader([]byte(`{"idle_detection": {"enabled": false}}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/idle-detection", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/idle-detection", nil)
	delRec := httptest.NewRecorder()
	e.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusOK, delRec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(delRec.Body.Bytes(), &resp))
	idle := resp["idle_detection"].(map[string]any)
	assert.Equal(t, true, idle["enabled"])
}
