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
