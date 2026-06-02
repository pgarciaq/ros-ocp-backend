package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupSnapshotSettingsTestEcho(t *testing.T, orgID string) *echo.Echo {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

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
	v1.GET("/recommendations/openshift/settings/snapshot", GetSnapshotSettings)
	v1.PUT("/recommendations/openshift/settings/snapshot", PutSnapshotSettings)
	v1.DELETE("/recommendations/openshift/settings/snapshot", DeleteSnapshotSettings)
	return e
}

func TestGetSnapshotSettings_ReturnsDefaults(t *testing.T) {
	orgID := "org-snapshot-settings-get-" + uuid.New().String()[:8]
	e := setupSnapshotSettingsTestEcho(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/snapshot", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, float64(engine.SnapshotSettingsDefaults.StaleDays), resp["stale_days"])
	assert.Equal(t, float64(engine.SnapshotSettingsDefaults.InventoryFreshHours), resp["inventory_fresh_hours"])
}

func TestPutSnapshotSettings_UpdatesSettings(t *testing.T) {
	orgID := "org-snapshot-settings-put-" + uuid.New().String()[:8]
	e := setupSnapshotSettingsTestEcho(t, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	putBody := bytes.NewReader([]byte(`{"stale_days": 120, "inventory_fresh_hours": 12}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/snapshot", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(putRec.Body.Bytes(), &resp))
	assert.Equal(t, float64(120), resp["stale_days"])
	assert.Equal(t, float64(12), resp["inventory_fresh_hours"])
}

func TestPutSnapshotSettings_TriggersAsyncRecalculation(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_THRESHOLD_RECALCULATION_ENABLED", "true")

	orgID := "org-snapshot-settings-recalc-" + uuid.New().String()[:8]
	e := setupSnapshotSettingsTestEcho(t, orgID)

	var triggeredOrg, triggeredType string
	engine.SetThresholdRecalcHookForTest(func(oid, rt string) {
		triggeredOrg = oid
		triggeredType = rt
	})
	defer engine.ClearThresholdRecalcHookForTest()

	putBody := bytes.NewReader([]byte(`{"stale_days": 100}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/snapshot", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)
	assert.Equal(t, orgID, triggeredOrg)
	assert.Equal(t, "snapshot", triggeredType)
}

func TestPutSnapshotSettings_ForbiddenWhenEnvLocksField(t *testing.T) {
	t.Setenv("ROS_SNAPSHOT_STALE_DAYS", "90")
	orgID := "org-snapshot-settings-locked-" + uuid.New().String()[:8]
	e := setupSnapshotSettingsTestEcho(t, orgID)

	putBody := bytes.NewReader([]byte(`{"stale_days": 120}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/snapshot", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusForbidden, putRec.Code)
}

func TestPutSnapshotSettings_RejectsInvalidValues(t *testing.T) {
	orgID := "org-snapshot-settings-invalid-" + uuid.New().String()[:8]
	e := setupSnapshotSettingsTestEcho(t, orgID)

	putBody := bytes.NewReader([]byte(`{"orphan_age_days": 0}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/snapshot", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusBadRequest, putRec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(putRec.Body.Bytes(), &resp))
	assert.Equal(t, "error", resp["status"])
	errs, ok := resp["validation_errors"].([]any)
	require.True(t, ok)
	assert.NotEmpty(t, errs)
}

func TestDeleteSnapshotSettings_ResetsToDefaults(t *testing.T) {
	orgID := "org-snapshot-settings-delete-" + uuid.New().String()[:8]
	e := setupSnapshotSettingsTestEcho(t, orgID)

	putBody := bytes.NewReader([]byte(`{"stale_days": 120}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/snapshot", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/snapshot", nil)
	delRec := httptest.NewRecorder()
	e.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusNoContent, delRec.Code)

	getReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/snapshot", nil)
	getRec := httptest.NewRecorder()
	e.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &resp))
	assert.Equal(t, float64(engine.SnapshotSettingsDefaults.StaleDays), resp["stale_days"])
}
