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
)

func setupVMSettingsPUTHandler(t *testing.T, orgID string) *echo.Echo {
	t.Helper()
	e := setupVMRecommendationsHandler(t, orgID)
	v1 := e.Group("/api/cost-management/v1")
	v1.PUT("/recommendations/openshift/settings/vm", PutVMSettings)
	return e
}

func TestVMSettings_PUT_ValidUpdate(t *testing.T) {
	orgID := "org-vm-settings-put-" + uuid.New().String()[:8]
	e := setupVMSettingsPUTHandler(t, orgID)

	body := bytes.NewReader([]byte(`{"disk": {"projection_window_days": 21}}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/vm", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	disk, ok := resp["disk"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(21), disk["projection_window_days"])
}

func TestVMSettings_PUT_InvalidJSON(t *testing.T) {
	orgID := "org-vm-settings-badjson-" + uuid.New().String()[:8]
	e := setupVMSettingsPUTHandler(t, orgID)

	body := bytes.NewReader([]byte(`{not json`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/vm", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestVMSettings_PUT_OutOfRangeValues(t *testing.T) {
	orgID := "org-vm-settings-range-" + uuid.New().String()[:8]
	e := setupVMSettingsPUTHandler(t, orgID)

	body := bytes.NewReader([]byte(`{"thresholds": {"cpu_percentile_cost": 2.0}}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/vm", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "error", resp["status"])
}

func TestVMSettings_GET_IncludesGPUClassificationThresholds(t *testing.T) {
	orgID := "org-vm-settings-gpu-get-" + uuid.New().String()[:8]
	e := setupVMSettingsPUTHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/vm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	gpu, ok := resp["gpu"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(500), gpu["idle_threshold_bp"])
	assert.Equal(t, float64(3000), gpu["underutil_threshold_bp"])
	assert.Equal(t, float64(0), gpu["fb_saturation_mib"])
	assert.Equal(t, float64(8500), gpu["compute_saturation_threshold_bp"])
}

func TestVMSettings_GET_IncludesPlacementBlock(t *testing.T) {
	orgID := "org-vm-settings-placement-" + uuid.New().String()[:8]
	e := setupVMSettingsPUTHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/vm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	placement, ok := resp["placement"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, placement["enable_placement_checks"])
	assert.Equal(t, float64(3), placement["placement_skew_ratio"])
	assert.Equal(t, true, placement["enable_shared_pvc_correlation"])
	assert.Equal(t, float64(64), placement["numa_node_memory_gib"])
}

func TestVMSettings_PUT_PlacementBlock(t *testing.T) {
	orgID := "org-vm-settings-placement-put-" + uuid.New().String()[:8]
	e := setupVMSettingsPUTHandler(t, orgID)

	body := bytes.NewReader([]byte(`{
		"placement": {
			"placement_skew_ratio": 4,
			"numa_node_memory_gib": 48
		}
	}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/vm", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	placement, ok := resp["placement"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(4), placement["placement_skew_ratio"])
	assert.Equal(t, float64(48), placement["numa_node_memory_gib"])
}

func TestVMSettings_PUT_GPUClassificationThresholds(t *testing.T) {
	orgID := "org-vm-settings-gpu-put-" + uuid.New().String()[:8]
	e := setupVMSettingsPUTHandler(t, orgID)

	body := bytes.NewReader([]byte(`{
		"gpu": {
			"idle_threshold_bp": 600,
			"underutil_threshold_bp": 3500,
			"fb_saturation_mib": 8192,
			"compute_saturation_threshold_bp": 8800
		}
	}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/vm", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	gpu, ok := resp["gpu"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(600), gpu["idle_threshold_bp"])
	assert.Equal(t, float64(3500), gpu["underutil_threshold_bp"])
	assert.Equal(t, float64(8192), gpu["fb_saturation_mib"])
	assert.Equal(t, float64(8800), gpu["compute_saturation_threshold_bp"])
}

func TestVMSettings_PUT_PartialUpdate(t *testing.T) {
	orgID := "org-vm-settings-partial-" + uuid.New().String()[:8]
	e := setupVMSettingsPUTHandler(t, orgID)

	getReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/vm", nil)
	getRec := httptest.NewRecorder()
	e.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	var before map[string]any
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &before))
	beforeThresholds := before["thresholds"]

	patch := bytes.NewReader([]byte(`{"io": {"high_iops_threshold": 9999}}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/vm", patch)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code, putRec.Body.String())

	var after map[string]any
	require.NoError(t, json.Unmarshal(putRec.Body.Bytes(), &after))
	io, ok := after["io"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(9999), io["high_iops_threshold"])
	assert.Equal(t, beforeThresholds, after["thresholds"])
}
