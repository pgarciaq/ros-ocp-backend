package api

import (
	"bytes"
	"encoding/json"
	"errors"
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
	t.Cleanup(config.ResetForTest) // rebuild config from restored env — no RBACEnabled leak into later tests
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
		validBHPayload())
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

func TestRequireSettingsWrite_DeniedStopsCaller(t *testing.T) {
	for _, tc := range []struct {
		name  string
		perms map[string][]string
	}{
		{"read_only", readOnlyPermissions()},
		{"absent_permissions", nil},
		{"settings_read_only", map[string][]string{"settings.read": {"*"}}},
		{"non_wildcard_write", map[string][]string{"settings.write": {"one-project"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(config.ResetForTest)
			t.Setenv("RBAC_ENABLE", "true")
			config.ResetForTest()
			e := echo.New()
			var guardErr error
			effects := 0
			e.PUT("/settings", func(c echo.Context) error {
				if tc.perms != nil {
					c.Set("user.permissions", tc.perms)
				}
				guardErr = requireSettingsWrite(c)
				if guardErr != nil {
					return guardErr
				}
				effects++
				return c.NoContent(http.StatusNoContent)
			})
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/settings", nil))
			// A successful JSON write must not turn a permission denial into nil.
			assert.Error(t, guardErr, "denial must stop the caller even when the 403 response write succeeds")
			// The pre-fix nil error reaches the protected action despite status 403.
			assert.Zero(t, effects, "forbidden request reached the protected action")
			require.Equal(t, http.StatusForbidden, rec.Code)
			assert.JSONEq(t, `{"status":"error","message":"User does not have permission to modify settings"}`, rec.Body.String())
		})
	}
}

func TestRequireSettingsWrite_Allowed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled string
		perms   map[string][]string
	}{
		{"settings_writer", "true", settingsWritePermissions()},
		{"global_wildcard", "true", map[string][]string{"*": {}}},
		{"rbac_disabled", "false", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(config.ResetForTest)
			t.Setenv("RBAC_ENABLE", tc.enabled)
			config.ResetForTest()
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(httptest.NewRequest(http.MethodPut, "/settings", nil), rec)
			if tc.perms != nil {
				c.Set("user.permissions", tc.perms)
			}
			require.NoError(t, requireSettingsWrite(c))
			assert.False(t, c.Response().Committed, "allowed writes must not emit a denial")
			assert.Empty(t, rec.Body.String())
		})
	}
}

type settingsRBACFailingWriter struct {
	header http.Header
	err    error
}

func (w *settingsRBACFailingWriter) Header() http.Header       { return w.header }
func (w *settingsRBACFailingWriter) WriteHeader(int)           {}
func (w *settingsRBACFailingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRequireSettingsWrite_ResponseWriteFailure(t *testing.T) {
	t.Cleanup(config.ResetForTest)
	t.Setenv("RBAC_ENABLE", "true")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest) // rebuild config from restored env — no RBACEnabled leak into later tests
	want := errors.New("test response write failure")
	w := &settingsRBACFailingWriter{header: make(http.Header), err: want}
	c := echo.New().NewContext(httptest.NewRequest(http.MethodPut, "/settings", nil), w)
	c.Set("user.permissions", readOnlyPermissions())
	// Returning an authorization sentinel must not hide the original I/O error.
	require.ErrorIs(t, requireSettingsWrite(c), want)
}
