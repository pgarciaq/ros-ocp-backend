package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"

	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins"
)

type settingsLockedEndpointCase struct {
	name    string
	getPath string
	putPath string
	putBody string
}

func setupAllSettingsLockedTestEcho(t *testing.T, orgID string) *echo.Echo {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetForTest()
	t.Setenv("ROS_ENABLE_VM_RECS", "true")
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
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
	api.RegisterV1RoutesForTest(v1, nil)
	return e
}

func enableGlobalSettingsLock(t *testing.T) {
	t.Helper()
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	t.Setenv("ROS_ENABLE_VM_RECS", "true")
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	_ = config.GetConfig()
}

func allSettingsLockedEndpointCases() []settingsLockedEndpointCase {
	return []settingsLockedEndpointCase{
		{
			name:    "thresholds_container",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container",
			putBody: `{"min_margin": 1.25}`,
		},
		{
			name:    "thresholds_gpu",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=gpu",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=gpu",
			putBody: `{"min_margin": 1.25}`,
		},
		{
			name:    "thresholds_node",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=node",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=node",
			putBody: `{"min_margin": 1.25}`,
		},
		{
			name:    "thresholds_namespace",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=namespace",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=namespace",
			putBody: `{"min_margin": 1.25}`,
		},
		{
			name:    "thresholds_pvc",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=pvc",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=pvc",
			putBody: `{"min_margin": 1.25}`,
		},
		{
			name:    "vm_settings",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/vm",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/vm",
			putBody: `{"disk": {"projection_window_days": 22}}`,
		},
		{
			name:    "vm_terms",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/vm/terms",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/vm/terms",
			putBody: `{"terms": [{"name": "short_term", "window_days": 14}]}`,
		},
		{
			name:    "quota",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/quota",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/quota",
			putBody: `{"headroom_percent": 15, "high_risk_threshold_percent": 90, "medium_risk_threshold_percent": 70}`,
		},
		{
			name:    "cluster_quota",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/cluster-quota",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/cluster-quota",
			putBody: `{"headroom_percent": 15, "high_risk_threshold_percent": 90, "medium_risk_threshold_percent": 70}`,
		},
		{
			name:    "idle_detection",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/idle-detection",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/idle-detection",
			putBody: `{"idle_detection": {"enabled": false}}`,
		},
		{
			name:    "snapshot",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/snapshot",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/snapshot",
			putBody: `{"stale_days": 120}`,
		},
		{
			name:    "terms_container",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container",
			putBody: `{"terms": [{"name": "short", "window_days": 7}]}`,
		},
		{
			name:    "terms_vm",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=vm",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=vm",
			putBody: `{"terms": [{"name": "short", "window_days": 7}]}`,
		},
		{
			name:    "business_hours",
			getPath: "/api/cost-management/v1/recommendations/openshift/settings/business-hours",
			putPath: "/api/cost-management/v1/recommendations/openshift/settings/business-hours",
			putBody: `{"timezone": "America/New_York", "schedule": {"days": ["monday"], "start_time": "08:00", "end_time": "17:00"}, "enabled": true}`,
		},
	}
}

func deletePathForSettingsCase(tc settingsLockedEndpointCase) string {
	if tc.putPath != "" {
		return tc.putPath
	}
	return tc.getPath
}

func TestSettingsLocked_AllEndpoints_TableDriven(t *testing.T) {
	orgID := "org-settings-locked-all-" + uuid.New().String()[:8]
	e := setupAllSettingsLockedTestEcho(t, orgID)
	enableGlobalSettingsLock(t)

	for _, tc := range allSettingsLockedEndpointCases() {
		tc := tc
		t.Run(tc.name+"/PUT_returns_403", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, tc.putPath, bytes.NewReader([]byte(tc.putBody)))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
			assertSettingsLockedForbiddenBody(t, rec)
		})

		t.Run(tc.name+"/DELETE_returns_403", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, deletePathForSettingsCase(tc), nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
			assertSettingsLockedForbiddenBody(t, rec)
		})

		t.Run(tc.name+"/GET_returns_settings_locked", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.getPath, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var resp map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			locked, ok := resp["settings_locked"].(bool)
			require.True(t, ok, "settings_locked missing in GET %s: %s", tc.getPath, rec.Body.String())
			assert.True(t, locked)
		})
	}
}

func assertSettingsLockedForbiddenBody(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, true, body["locked"])
	errMsg, ok := body["error"].(string)
	require.True(t, ok)
	assert.Contains(t, errMsg, "locked")
}

func TestSettingsLocked_DELETE_VM_Returns403(t *testing.T) {
	orgID := "org-settings-locked-del-vm-" + uuid.New().String()[:8]
	e := setupAllSettingsLockedTestEcho(t, orgID)
	enableGlobalSettingsLock(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/vm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assertSettingsLockedForbiddenBody(t, rec)
}

func TestSettingsLocked_DELETE_Snapshot_Returns403(t *testing.T) {
	orgID := "org-settings-locked-del-snap-" + uuid.New().String()[:8]
	e := setupAllSettingsLockedTestEcho(t, orgID)
	enableGlobalSettingsLock(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/snapshot", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assertSettingsLockedForbiddenBody(t, rec)
}

func TestSettingsLocked_DELETE_Terms_Returns403(t *testing.T) {
	orgID := "org-settings-locked-del-terms-" + uuid.New().String()[:8]
	e := setupAllSettingsLockedTestEcho(t, orgID)
	enableGlobalSettingsLock(t)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assertSettingsLockedForbiddenBody(t, rec)
}

func TestSettingsLocked_Terms_VMLockWithoutGenericTermsLock(t *testing.T) {
	orgID := "org-settings-locked-terms-vm-" + uuid.New().String()[:8]
	e := setupAllSettingsLockedTestEcho(t, orgID)

	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	t.Setenv("ROS_SETTINGS_LOCKED_TERMS", "false")
	t.Setenv("ROS_SETTINGS_LOCKED_VM", "true")
	t.Setenv("ROS_ENABLE_VM_RECS", "true")
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	_ = config.GetConfig()

	putBody := bytes.NewReader([]byte(`{"terms": [{"name": "short", "window_days": 7}]}`))
	putReq := httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=vm", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusForbidden, putRec.Code)

	getReq := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=vm", nil)
	getRec := httptest.NewRecorder()
	e.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &resp))
	assert.True(t, resp["settings_locked"].(bool))
}

func TestSettingsLocked_StartupLog(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	_ = config.GetConfig()

	var msgs []string
	engine.LogSettingsLockedStartup(func(format string, args ...any) {
		msgs = append(msgs, fmt.Sprintf(format, args...))
	})

	require.NotEmpty(t, msgs)
	assert.True(t, strings.Contains(msgs[0], "ROS_SETTINGS_LOCKED=true"),
		"first log line: %q", msgs[0])
}
