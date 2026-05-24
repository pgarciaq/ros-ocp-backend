package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestGetSavingsSummary_Unauthorized_Returns401(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := GetFleetSavingsSummary(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func setupSavingsSummaryHandler(t *testing.T, orgID string) *echo.Echo {
	t.Helper()
	pool := testutil.SetupTestDB(t)
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
	v1.GET("/recommendations/openshift/savings-summary", GetFleetSavingsSummary)
	return e
}

func TestGetSavingsSummary_ValidAuth_ReturnsStructure(t *testing.T) {
	orgID := "org-savings-summary-structure"
	e := setupSavingsSummaryHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var summary FleetSavingsSummaryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &summary))

	assert.Equal(t, "USD", summary.Currency)
	assert.NotNil(t, summary.ByCluster)
	assert.NotNil(t, summary.ByPlugin)
	assert.Equal(t, 0.0, summary.TotalEstimatedMonthlySavings)
}

func TestGetSavingsSummary_EngineFilterCost_Accepted(t *testing.T) {
	orgID := "org-savings-summary-cost"
	e := setupSavingsSummaryHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary?engine=cost", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

func TestGetSavingsSummary_EngineFilterPerformance_Accepted(t *testing.T) {
	orgID := "org-savings-summary-perf"
	e := setupSavingsSummaryHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary?engine=performance", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

func TestGetSavingsSummary_InvalidEngine_Returns400(t *testing.T) {
	orgID := "org-savings-summary-bad-engine"
	e := setupSavingsSummaryHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary?engine=invalid", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "error", body["status"])
	assert.Contains(t, body["message"], "invalid engine")
}

func TestGetSavingsSummary_EmptyFleet_ReturnsZeros(t *testing.T) {
	orgID := "org-savings-summary-empty"
	e := setupSavingsSummaryHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var summary FleetSavingsSummaryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &summary))

	assert.Equal(t, 0.0, summary.TotalEstimatedMonthlySavings)
	assert.Empty(t, summary.ByCluster)
	assert.Equal(t, 0.0, summary.ByPlugin.Container)
	assert.Equal(t, 0.0, summary.ByPlugin.GPU)
	assert.Equal(t, 0.0, summary.ByPlugin.Node)
	assert.Equal(t, 0.0, summary.ByPlugin.PVC)
	assert.Equal(t, 0.0, summary.ByPlugin.Snapshot)
}

func TestGetFleetSavingsSummary_NoPool_Returns503(t *testing.T) {
	origPool := database.Pool
	database.Pool = nil
	t.Cleanup(func() { database.Pool = origPool })

	c, rec := newHandlerContext(t, http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary")

	err := GetFleetSavingsSummary(c)
	if err != nil {
		t.Fatalf("handler returned Go error: %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rec.Code)
	}
}