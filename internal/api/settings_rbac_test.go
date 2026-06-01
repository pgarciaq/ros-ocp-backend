package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func enableSettingsRBACForTest(t *testing.T) {
	t.Helper()
	t.Setenv("RBAC_ENABLE", "true")
	config.ResetForTest()
}

func setupSettingsRBACEcho(t *testing.T, pool *pgxpool.Pool, orgID string, perms map[string][]string) *echo.Echo {
	t.Helper()
	enableSettingsRBACForTest(t)
	config.ResetForTest()
	engine.InitThresholdDefaults(config.GetConfig())
	if pool != nil {
		prev := db.Pool
		db.Pool = pool
		t.Cleanup(func() { db.Pool = prev })
	}

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("Identity", identity.XRHID{
				Identity: identity.Identity{OrgID: orgID},
			})
			if perms != nil {
				c.Set("user.permissions", perms)
			}
			return next(c)
		}
	})
	v1 := e.Group("/api/cost-management/v1")
	RegisterThresholdSettingsRoutes(v1)
	v1.GET("/recommendations/openshift/settings/terms", GetTermSettings)
	v1.PUT("/recommendations/openshift/settings/terms", PutTermSettings)
	v1.GET("/recommendations/openshift/settings/snapshot", GetSnapshotSettings)
	v1.PUT("/recommendations/openshift/settings/snapshot", PutSnapshotSettings)
	RegisterBusinessHoursRoutes(v1, NewBusinessHoursSettingsHandler(&recordingReshipTrigger{}))
	return e
}

func readOnlyPermissions() map[string][]string {
	return map[string][]string{
		"openshift.cluster": {"*"},
	}
}

func settingsWritePermissions() map[string][]string {
	return map[string][]string{
		"openshift.cluster": {"*"},
		"settings.write":    {"*"},
	}
}

func TestThresholdSettings_GET_ReadOnlyUser_Succeeds(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	e := setupSettingsRBACEcho(t, pool, "org-rbac-threshold-get", readOnlyPermissions())

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestThresholdSettings_PUT_ReadOnlyUser_Returns403(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	e := setupSettingsRBACEcho(t, pool, "org-rbac-threshold-put-deny", readOnlyPermissions())

	body := bytes.NewReader([]byte(`{"cpu_cost_percentile": 0.72}`))
	req := httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestThresholdSettings_DELETE_ReadOnlyUser_Returns403(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	e := setupSettingsRBACEcho(t, pool, "org-rbac-threshold-del-deny", readOnlyPermissions())

	req := httptest.NewRequest(http.MethodDelete,
		"/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestThresholdSettings_PUT_WriteUser_Succeeds(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-rbac-threshold-put-allow"
	e := setupSettingsRBACEcho(t, pool, orgID, settingsWritePermissions())

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	body := bytes.NewReader([]byte(`{"cpu_cost_percentile": 0.72}`))
	req := httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.72, resp["cpu_cost_percentile"].(float64), 1e-9)
}

func TestBHSettings_PUT_ReadOnlyUser_Returns403(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	enableBusinessHoursForTest(t)
	enableSettingsRBACForTest(t)
	e := setupSettingsRBACEcho(t, pool, "org-rbac-bh-put-deny", readOnlyPermissions())

	rec := serveBH(t, e, http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/business-hours",
		"org-rbac-bh-put-deny", validBHPayload())
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestTermSettings_PUT_ReadOnlyUser_Returns403(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	e := setupSettingsRBACEcho(t, pool, "org-rbac-terms-put-deny", readOnlyPermissions())

	body := bytes.NewReader([]byte(`{"terms":[{"name":"short","window_days":2}]}`))
	req := httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestSnapshotSettings_PUT_ReadOnlyUser_Returns403(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	e := setupSettingsRBACEcho(t, pool, "org-rbac-snapshot-put-deny", readOnlyPermissions())

	body := bytes.NewReader([]byte(`{"orphan_age_days": 45}`))
	req := httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/snapshot", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}
