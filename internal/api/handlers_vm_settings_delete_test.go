package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

func setupVMSettingsCRUDHandler(t *testing.T, orgID string) *echo.Echo {
	t.Helper()
	e := setupVMRecommendationsHandler(t, orgID)
	v1 := e.Group("/api/cost-management/v1")
	v1.PUT("/recommendations/openshift/settings/vm", PutVMSettings)
	v1.DELETE("/recommendations/openshift/settings/vm", DeleteVMSettings)
	v1.PUT("/recommendations/openshift/settings/vm/terms", PutVMTermSettings)
	v1.DELETE("/recommendations/openshift/settings/vm/terms", DeleteVMTermSettings)
	return e
}

func TestDeleteVMSettings_ResetsToDefaults(t *testing.T) {
	orgID := "org-vm-settings-delete-" + uuid.New().String()[:8]
	e := setupVMSettingsCRUDHandler(t, orgID)

	putBody := bytes.NewReader([]byte(`{"disk": {"projection_window_days": 21}}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/vm", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/vm", nil)
	delRec := httptest.NewRecorder()
	e.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusNoContent, delRec.Code)

	getReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/vm", nil)
	getRec := httptest.NewRecorder()
	e.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &resp))
	disk, ok := resp["disk"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(engine.DefaultVMRecConfig().DiskProjectionWindowDays), disk["projection_window_days"])
}

func TestDeleteVMSettings_WhenNoOverride_Returns204(t *testing.T) {
	orgID := "org-vm-settings-delete-empty-" + uuid.New().String()[:8]
	e := setupVMSettingsCRUDHandler(t, orgID)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/vm", nil)
	delRec := httptest.NewRecorder()
	e.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusNoContent, delRec.Code)
}

func TestDeleteVMTerms_ResetsToDefaults(t *testing.T) {
	orgID := "org-vm-terms-delete-" + uuid.New().String()[:8]
	e := setupVMSettingsCRUDHandler(t, orgID)

	putBody := bytes.NewReader([]byte(`{"terms": [{"name": "short_term", "window_days": 14}]}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/vm/terms", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/vm/terms", nil)
	delRec := httptest.NewRecorder()
	e.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusOK, delRec.Code)

	var resp vmTermSettingsResponse
	require.NoError(t, json.Unmarshal(delRec.Body.Bytes(), &resp))
	require.Len(t, resp.Terms, 3)
	defaults := engine.DefaultTermsForPlugin("vm")
	assert.Equal(t, defaults[0].WindowDays, resp.Terms[0].WindowDays)
}
