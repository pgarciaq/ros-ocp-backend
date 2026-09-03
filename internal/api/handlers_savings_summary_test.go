package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestFleetSavingsByClusterSQL_NoRHAccountsJoin(t *testing.T) {
	sql := fleetSavingsByClusterSQL("", "$2", "$3", "$4", "1")
	lower := strings.ToLower(sql)
	assert.Contains(t, lower, "left join clusters")
	assert.Contains(t, lower, "cluster_alias")
	assert.Contains(t, lower, "c.org_id = $1")
	assert.NotContains(t, lower, "rh_accounts")
}

func TestGetSavingsSummary_Unauthorized_Returns401(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := GetFleetSavingsSummary(c)
	assert.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func setupSavingsSummaryHandler(t *testing.T, orgID string) *echo.Echo {
	t.Helper()
	fleetsummary.ResetForTest()
	t.Cleanup(fleetsummary.ResetForTest)
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
	assert.Equal(t, "0.00", summary.EstimatedMonthlySavings.Value)
	assert.Equal(t, "USD", summary.EstimatedMonthlySavings.Units)
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

func TestGetSavingsSummary_TermFilterShort_Accepted(t *testing.T) {
	orgID := "org-savings-summary-short-term"
	e := setupSavingsSummaryHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary?term=short", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

func TestGetSavingsSummary_InvalidTerm_Returns400(t *testing.T) {
	orgID := "org-savings-summary-bad-term"
	e := setupSavingsSummaryHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary?term=invalid", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Contains(t, body["message"], "invalid term")
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

func TestGetSavingsSummary_CacheHitOnSecondRequest(t *testing.T) {
	orgID := "org-savings-summary-cache-hit"
	e := setupSavingsSummaryHandler(t, orgID)

	beforeHits := savingsSummaryCacheHits(t)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary?engine=cost&term=medium", nil)
	rec1 := httptest.NewRecorder()
	e.ServeHTTP(rec1, req)
	require.Equal(t, http.StatusOK, rec1.Code)

	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req)
	require.Equal(t, http.StatusOK, rec2.Code)

	assert.Equal(t, beforeHits+1, savingsSummaryCacheHits(t))
}

func savingsSummaryCacheHits(t *testing.T) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != "rosocp_savings_summary_cache_hits_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			return m.GetCounter().GetValue()
		}
	}
	return 0
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

	assert.Equal(t, "0.00", summary.EstimatedMonthlySavings.Value)
	assert.Equal(t, "USD", summary.EstimatedMonthlySavings.Units)
	assert.Empty(t, summary.ByCluster)
	assert.Equal(t, "0.00", summary.ByPlugin.Container.Value)
	assert.Equal(t, "USD", summary.ByPlugin.Container.Units)
	assert.Equal(t, "0.00", summary.ByPlugin.GPU.Value)
	assert.Equal(t, "0.00", summary.ByPlugin.Node.Value)
	assert.Equal(t, "0.00", summary.ByPlugin.PVC.Value)
	assert.Equal(t, "0.00", summary.ByPlugin.Snapshot.Value)
}

func TestGetFleetSavingsSummary_NoPool_Returns503(t *testing.T) {
	restore := database.SuspendForceTestPool()
	t.Cleanup(restore)
	database.Pool = nil

	c, rec := newHandlerContext(t, http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary")

	err := GetFleetSavingsSummary(c)
	if err != nil {
		t.Fatalf("handler returned Go error: %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rec.Code)
	}
}
