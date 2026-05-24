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
	v1.GET("/recommendations/openshift/settings/thresholds", GetThresholdSettings)
	v1.PUT("/recommendations/openshift/settings/thresholds", PutThresholdSettings)
	v1.DELETE("/recommendations/openshift/settings/thresholds", DeleteThresholdSettings)
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

func TestPutThresholdSettings_UpdatesAndReturnsMerged(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-api-put"
	e := setupThresholdTestEcho(t, pool, orgID)

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
