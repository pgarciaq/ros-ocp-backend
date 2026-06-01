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
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupSettingsLockedTestEcho(t *testing.T, pool *pgxpool.Pool, orgID string) *echo.Echo {
	t.Helper()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

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
	RegisterThresholdSettingsRoutes(v1)
	v1.GET("/recommendations/openshift/settings/vm", GetVMSettings)
	v1.PUT("/recommendations/openshift/settings/vm", PutVMSettings)
	v1.GET("/recommendations/openshift/settings/business-hours", NewBusinessHoursSettingsHandler(nil).GetOrgDefault)
	return e
}

func TestSettingsLocked_PUT_Returns403(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-settings-locked-put"
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	_ = config.GetConfig()
	e := setupSettingsLockedTestEcho(t, pool, orgID)

	body := bytes.NewReader([]byte(`{"min_margin": 1.25}`))
	req := httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestSettingsLocked_DELETE_Returns403(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-settings-locked-del"
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	_ = config.GetConfig()
	e := setupSettingsLockedTestEcho(t, pool, orgID)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestSettingsLocked_GET_ReturnsDefaults(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-settings-locked-get"
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	_ = config.GetConfig()
	e := setupSettingsLockedTestEcho(t, pool, orgID)

	putBody := bytes.NewReader([]byte(`{"min_margin": 1.99}`))
	putReq := httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	// PUT is blocked under global lock; insert override directly.
	ctx := putReq.Context()
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'container', '{"min_margin": 1.99}'::jsonb)
		ON CONFLICT (org_id, recommendation_type) DO UPDATE SET thresholds = EXCLUDED.thresholds`,
		orgID)
	require.NoError(t, err)

	getReq := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", nil)
	getRec := httptest.NewRecorder()
	e.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &resp))
	assert.InDelta(t, 1.15, resp["min_margin"].(float64), 1e-9)
	assert.True(t, resp["settings_locked"].(bool))
}

func TestSettingsLocked_GET_ShowsLockedFields(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-settings-locked-fields"
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	_ = config.GetConfig()
	e := setupSettingsLockedTestEcho(t, pool, orgID)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	locked, ok := resp["locked_fields"].([]any)
	require.True(t, ok)
	require.Len(t, locked, 1)
	assert.Equal(t, "*", locked[0])
}

func TestSettingsLocked_PerFeatureOptOut(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-settings-locked-vm-optout"
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	t.Setenv("ROS_SETTINGS_LOCKED_VM", "false")
	_ = config.GetConfig()
	e := setupSettingsLockedTestEcho(t, pool, orgID)

	vmBody := bytes.NewReader([]byte(`{"disk": {"projection_window_days": 22}}`))
	vmReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/vm", vmBody)
	vmReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	vmRec := httptest.NewRecorder()
	e.ServeHTTP(vmRec, vmReq)
	require.Equal(t, http.StatusOK, vmRec.Code, vmRec.Body.String())

	containerBody := bytes.NewReader([]byte(`{"min_margin": 1.25}`))
	containerReq := httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", containerBody)
	containerReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	containerRec := httptest.NewRecorder()
	e.ServeHTTP(containerRec, containerReq)
	require.Equal(t, http.StatusForbidden, containerRec.Code)
}

func TestSettingsLocked_EnvVarStillWins(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-settings-locked-env"
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	t.Setenv("ROS_CONTAINER_CPU_COST_PERCENTILE", "0.99")
	_ = config.GetConfig()
	e := setupSettingsLockedTestEcho(t, pool, orgID)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.99, resp["cpu_cost_percentile"].(float64), 1e-9)
}

func TestSettingsLocked_BusinessHours_Disabled(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-settings-locked-bh"
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	_ = config.GetConfig()
	e := setupSettingsLockedTestEcho(t, pool, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.False(t, resp["enabled"].(bool))
	assert.True(t, resp["settings_locked"].(bool))
}
