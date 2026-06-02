package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
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

func setupThresholdTestEcho(t *testing.T, pool *pgxpool.Pool, orgID string) *echo.Echo {
	t.Helper()
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
			return next(c)
		}
	})
	v1 := e.Group("/api/cost-management/v1")
	RegisterThresholdSettingsRoutes(v1)
	return e
}

func TestGetThresholdSettings_ReturnsDefaults(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-get"
	e := setupThresholdTestEcho(t, pool, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.60, resp["cpu_cost_percentile"].(float64), 1e-9)
	locked, ok := resp["locked_fields"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, locked)
}

func TestPutThresholdSettings_TriggersAsyncRecalculation(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_THRESHOLD_RECALCULATION_ENABLED", "true")

	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-recalc"
	e := setupThresholdTestEcho(t, pool, orgID)

	var mu sync.Mutex
	var triggeredOrg, triggeredType string
	engine.SetThresholdRecalcHookForTest(func(oid, rt string) {
		mu.Lock()
		triggeredOrg = oid
		triggeredType = rt
		mu.Unlock()
	})
	defer engine.ClearThresholdRecalcHookForTest()

	body := bytes.NewReader([]byte(`{"cpu_cost_percentile": 0.72}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, orgID, triggeredOrg)
	assert.Equal(t, "container", triggeredType)
}

func TestPutThresholdSettings_UpdatesAndReturnsMerged(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-put"
	e := setupThresholdTestEcho(t, pool, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	body := bytes.NewReader([]byte(`{"cpu_cost_percentile": 0.71}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.71, resp["cpu_cost_percentile"].(float64), 1e-9)
}

func TestPutThresholdSettings_RejectsOutOfRange(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-bad-range"
	e := setupThresholdTestEcho(t, pool, orgID)

	body := bytes.NewReader([]byte(`{"cpu_cost_percentile": 2.0}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "error", resp["status"])
	assert.Contains(t, resp["message"], "cpu_cost_percentile")
	errs, ok := resp["validation_errors"].([]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, errs)
}

func TestPutThresholdSettings_RejectsMinMarginGreaterThanMax(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-min-max"
	e := setupThresholdTestEcho(t, pool, orgID)

	body := bytes.NewReader([]byte(`{"min_margin": 2.5, "max_margin": 1.5}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp["message"], "min_margin")
}

func TestPutThresholdSettings_ValidationBeforeLockedField(t *testing.T) {
	t.Setenv("ROS_CONTAINER_CPU_COST_PERCENTILE", "0.65")
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-validate-first"
	e := setupThresholdTestEcho(t, pool, orgID)

	body := bytes.NewReader([]byte(`{"cpu_cost_percentile": 5.0}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPutThresholdSettings_ForbiddenWhenEnvLocksField(t *testing.T) {
	t.Setenv("ROS_CONTAINER_CPU_COST_PERCENTILE", "0.65")
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-forbidden"
	e := setupThresholdTestEcho(t, pool, orgID)

	body := bytes.NewReader([]byte(`{"cpu_cost_percentile": 0.55}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestGetThresholdSettings_Node_ReturnsDefaults(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-get-node"
	e := setupThresholdTestEcho(t, pool, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=node", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.80, resp["cost_target_utilization"].(float64), 1e-9)
	assert.InDelta(t, 0.30, resp["underutil_threshold"].(float64), 1e-9)
	assert.InDelta(t, 1.50, resp["overcommit_threshold"].(float64), 1e-9)
	assert.InDelta(t, 0.15, resp["pod_headroom_consolidation_gate"].(float64), 1e-9)
	assert.InDelta(t, 0.10, resp["pod_headroom_notification_threshold"].(float64), 1e-9)
	locked, ok := resp["locked_fields"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, locked)
}

func TestGetThresholdSettings_GPU_ReturnsDefaults(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-get-gpu"
	e := setupThresholdTestEcho(t, pool, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=gpu", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.02, resp["idle_threshold"].(float64), 1e-9)
	assert.InDelta(t, 0.25, resp["underutilized_sm_threshold"].(float64), 1e-9)
	assert.InDelta(t, 0.30, resp["compute_bound_dram_threshold"].(float64), 1e-9)
	assert.InDelta(t, 0.98, resp["mig_fb_percentile"].(float64), 1e-9)
	locked, ok := resp["locked_fields"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, locked)
}

func TestGetThresholdSettings_PVC_ReturnsDefaults(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-get-pvc"
	e := setupThresholdTestEcho(t, pool, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=pvc", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.20, resp["oversized_threshold"].(float64), 1e-9)
	assert.InDelta(t, 0.85, resp["near_full_threshold"].(float64), 1e-9)
	assert.InDelta(t, float64(2), resp["min_trend_days"].(float64), 1e-9)
	locked, ok := resp["locked_fields"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, locked)
}

func TestGetThresholdSettings_Namespace_ReturnsDefaults(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-get-namespace"
	e := setupThresholdTestEcho(t, pool, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=namespace", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.60, resp["cpu_cost_percentile"].(float64), 1e-9)
	assert.InDelta(t, 500.0, resp["mem_trend_slope_threshold"].(float64), 1e-9)
	locked, ok := resp["locked_fields"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, locked)
}

func TestPutThresholdSettings_Node_PodHeadroomRejectsInvalid(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-node-pod-headroom-bad"
	e := setupThresholdTestEcho(t, pool, orgID)

	body := bytes.NewReader([]byte(`{"pod_headroom_consolidation_gate": 0.05, "pod_headroom_notification_threshold": 0.10}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=node", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "pod_headroom_consolidation_gate")

	body = bytes.NewReader([]byte(`{"pod_headroom_consolidation_gate": 1.5}`))
	req = httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=node", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPutThresholdSettings_Node_PodHeadroomPersistsAndReturns(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-node-pod-headroom-put"
	e := setupThresholdTestEcho(t, pool, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	body := bytes.NewReader([]byte(`{"pod_headroom_consolidation_gate": 0.20, "pod_headroom_notification_threshold": 0.08}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=node", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.20, resp["pod_headroom_consolidation_gate"].(float64), 1e-9)
	assert.InDelta(t, 0.08, resp["pod_headroom_notification_threshold"].(float64), 1e-9)
}

func TestPutThresholdSettings_Node_PersistsAndReturns(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-put-node"
	e := setupThresholdTestEcho(t, pool, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	body := bytes.NewReader([]byte(`{"cost_target_utilization": 0.75}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=node", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.75, resp["cost_target_utilization"].(float64), 1e-9)
}

func TestPutThresholdSettings_Node_TriggersAsyncRecalculation(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_THRESHOLD_RECALCULATION_ENABLED", "true")

	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-recalc-node"
	e := setupThresholdTestEcho(t, pool, orgID)

	var mu sync.Mutex
	var triggeredOrg, triggeredType string
	engine.SetThresholdRecalcHookForTest(func(oid, rt string) {
		mu.Lock()
		triggeredOrg = oid
		triggeredType = rt
		mu.Unlock()
	})
	defer engine.ClearThresholdRecalcHookForTest()

	body := bytes.NewReader([]byte(`{"underutil_threshold": 0.28}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=node", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, orgID, triggeredOrg)
	assert.Equal(t, "node", triggeredType)
}

func TestPutThresholdSettings_Node_ForbiddenWhenEnvLocksField(t *testing.T) {
	t.Setenv("ROS_NODE_UNDERUTIL_THRESHOLD", "0.30")
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-forbidden-node"
	e := setupThresholdTestEcho(t, pool, orgID)

	body := bytes.NewReader([]byte(`{"underutil_threshold": 0.25}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=node", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestPutThresholdSettings_Node_IdleZombie_PersistsAndReturns(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-put-node-idle-zombie"
	e := setupThresholdTestEcho(t, pool, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	body := bytes.NewReader([]byte(`{
		"zombie_cpu_p95_mc": 180,
		"zombie_max_pods": 4,
		"idle_cpu_util_pct": 8,
		"idle_mem_util_pct": 9,
		"idle_max_pods": 12
	}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=node", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.Bytes())

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, float64(180), resp["zombie_cpu_p95_mc"])
	assert.Equal(t, float64(4), resp["zombie_max_pods"])
	assert.Equal(t, float64(8), resp["idle_cpu_util_pct"])
	assert.Equal(t, float64(9), resp["idle_mem_util_pct"])
	assert.Equal(t, float64(12), resp["idle_max_pods"])
}

func TestPutThresholdSettings_GPU_PersistsAndReturns(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-put-gpu"
	e := setupThresholdTestEcho(t, pool, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	body := bytes.NewReader([]byte(`{"idle_threshold": 0.05}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=gpu", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.05, resp["idle_threshold"].(float64), 1e-9)
}

func TestPutThresholdSettings_PVC_PersistsAndReturns(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-put-pvc"
	e := setupThresholdTestEcho(t, pool, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	body := bytes.NewReader([]byte(`{"oversized_threshold": 0.25}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=pvc", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.25, resp["oversized_threshold"].(float64), 1e-9)
}

func TestPutThresholdSettings_Namespace_PersistsAndReturns(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-put-namespace"
	e := setupThresholdTestEcho(t, pool, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	body := bytes.NewReader([]byte(`{"cpu_cost_percentile": 0.71}`))
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=namespace", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.71, resp["cpu_cost_percentile"].(float64), 1e-9)
}

func TestDeleteThresholdSettings_Node_ResetsDefaults(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-delete-node"
	e := setupThresholdTestEcho(t, pool, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	putBody := bytes.NewReader([]byte(`{"cost_target_utilization": 0.72}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=node", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=node", nil)
	delRec := httptest.NewRecorder()
	e.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusNoContent, delRec.Code)

	getReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=node", nil)
	getRec := httptest.NewRecorder()
	e.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.80, resp["cost_target_utilization"].(float64), 1e-9)
}

func TestDeleteThresholdSettings_GPU_ResetsDefaults(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-delete-gpu"
	e := setupThresholdTestEcho(t, pool, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	putBody := bytes.NewReader([]byte(`{"idle_threshold": 0.08}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=gpu", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=gpu", nil)
	delRec := httptest.NewRecorder()
	e.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusNoContent, delRec.Code)

	getReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=gpu", nil)
	getRec := httptest.NewRecorder()
	e.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.02, resp["idle_threshold"].(float64), 1e-9)
}

func TestDeleteThresholdSettings_PVC_ResetsDefaults(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-delete-pvc"
	e := setupThresholdTestEcho(t, pool, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	putBody := bytes.NewReader([]byte(`{"oversized_threshold": 0.30}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=pvc", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=pvc", nil)
	delRec := httptest.NewRecorder()
	e.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusNoContent, delRec.Code)

	getReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=pvc", nil)
	getRec := httptest.NewRecorder()
	e.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.20, resp["oversized_threshold"].(float64), 1e-9)
}

func TestDeleteThresholdSettings_Namespace_ResetsDefaults(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-delete-namespace"
	e := setupThresholdTestEcho(t, pool, orgID)

	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()

	putBody := bytes.NewReader([]byte(`{"cpu_cost_percentile": 0.68}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=namespace", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=namespace", nil)
	delRec := httptest.NewRecorder()
	e.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusNoContent, delRec.Code)

	getReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=namespace", nil)
	getRec := httptest.NewRecorder()
	e.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.60, resp["cpu_cost_percentile"].(float64), 1e-9)
}

func TestDeleteThresholdSettings_NoContent(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-delete"
	e := setupThresholdTestEcho(t, pool, orgID)

	putBody := bytes.NewReader([]byte(`{"min_margin": 1.25}`))
	putReq := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", putBody)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", nil)
	delRec := httptest.NewRecorder()
	e.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusNoContent, delRec.Code)

	getReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container", nil)
	getRec := httptest.NewRecorder()
	e.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &resp))
	assert.InDelta(t, 1.15, resp["min_margin"].(float64), 1e-9)
}

func TestGetDedicatedThresholdSettings_AllTypes(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	e := setupThresholdTestEcho(t, pool, "org-threshold-dedicated-get")

	for _, recType := range thresholdRecommendationTypes {
		t.Run(recType, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodGet,
				"/api/cost-management/v1/recommendations/openshift/settings/"+recType,
				nil,
			)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Empty(t, rec.Header().Get("Deprecation"))

			var resp map[string]interface{}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			assert.Contains(t, resp, "locked_fields")
		})
	}
}

func TestPutDeleteDedicatedThresholdSettings_Container(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	e := setupThresholdTestEcho(t, pool, "org-threshold-dedicated-crud")

	putBody := bytes.NewReader([]byte(`{"cpu_cost_percentile": 0.71}`))
	putReq := httptest.NewRequest(
		http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/container",
		putBody,
	)
	putReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	putRec := httptest.NewRecorder()
	e.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusOK, putRec.Code)
	assert.Empty(t, putRec.Header().Get("Deprecation"))

	getReq := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/container",
		nil,
	)
	getRec := httptest.NewRecorder()
	e.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.71, resp["cpu_cost_percentile"].(float64), 1e-9)

	delReq := httptest.NewRequest(
		http.MethodDelete,
		"/api/cost-management/v1/recommendations/openshift/settings/container",
		nil,
	)
	delRec := httptest.NewRecorder()
	e.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusNoContent, delRec.Code)
}

func TestGetThresholdSettings_DeprecatedAlias_ReturnsDeprecationHeaders(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	e := setupThresholdTestEcho(t, pool, "org-threshold-deprecated-alias")

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=gpu",
		nil,
	)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	assert.Equal(t, "true", rec.Header().Get("Deprecation"))
	assert.Equal(t, thresholdSuccessorLink("gpu"), rec.Header().Get("Link"))
}
