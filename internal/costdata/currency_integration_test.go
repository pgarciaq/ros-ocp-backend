package costdata_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

const (
	currencyClusterEUR = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1"
	currencyClusterUSD = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb2"
)

// savingsIntegrationMu serializes tests that call t.Setenv (process-global) and share
// api.getGPUCostProvider's singleton cache across mock Koku servers.
var savingsIntegrationMu sync.Mutex

func enableKokuMockForSavings(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("KOKU_MASU_URL", baseURL)
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	config.ResetForTest()
	_ = config.GetConfig()
}

func effectiveRatesResponse(currency, clusterID string) string {
	return fmt.Sprintf(`{
		"cluster_id": %q,
		"provider_uuid": "12345678-1234-1234-1234-123456789abc",
		"distribution_type": "cpu",
		"markup_pct": 10.0,
		"currency": %q,
		"configured_rates": {
			"cpu_core_usage_per_hour": {"infrastructure": 0.0, "supplementary": 0.007},
			"memory_gb_usage_per_hour": {"infrastructure": 0.0, "supplementary": 0.009},
			"node_cost_per_month": {"infrastructure": 1000.0, "supplementary": 0.0},
			"storage_gb_request_per_month": {"infrastructure": 0.0, "supplementary": 0.10}
		},
		"namespace_aggregates": {
			"koku": {
				"cost_model_cpu_cost": 100.0,
				"cost_model_memory_cost": 50.0,
				"infrastructure_cost": 500.0,
				"distributed_cost": 200.0,
				"cpu_usage_hours": 730.0,
				"cpu_request_hours": 1460.0,
				"mem_usage_hours": 365.0,
				"mem_request_hours": 730.0
			}
		}
	}`, clusterID, currency)
}

func setupSavingsSummaryWithMockKoku(t *testing.T, handler http.HandlerFunc) (*echo.Echo, context.Context, *pgxpool.Pool) {
	t.Helper()
	savingsIntegrationMu.Lock()
	t.Cleanup(func() {
		api.ResetGPUCostProviderForTest()
		costdata.ClearCostDataCacheForTest()
		config.ResetForTest()
		savingsIntegrationMu.Unlock()
	})
	api.ResetGPUCostProviderForTest()
	costdata.ClearCostDataCacheForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	enableKokuMockForSavings(t, srv.URL)

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})

	_, err = pool.Exec(ctx, `
		INSERT INTO rh_accounts (id, org_id) VALUES (1, $1)
		ON CONFLICT (id) DO UPDATE SET org_id = EXCLUDED.org_id`, testutil.TestOrgID)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/savings-summary", api.GetFleetSavingsSummary)

	return app, ctx, pool
}

func savingsSummaryIdentity(orgID string) string {
	id := map[string]interface{}{
		"identity": map[string]interface{}{
			"org_id":         orgID,
			"account_number": "test",
			"type":           "User",
			"user": map[string]interface{}{
				"username":     "test_user",
				"is_org_admin": true,
			},
		},
		"entitlements": map[string]interface{}{
			"cost_management": map[string]interface{}{"is_entitled": true},
		},
	}
	b, _ := json.Marshal(id)
	return base64.StdEncoding.EncodeToString(b)
}

func seedSavingsSummaryCluster(t *testing.T, ctx context.Context, pool *pgxpool.Pool, clusterUUID, alias string, containerSavingsUSD float64) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, $2, $3, now()) ON CONFLICT DO NOTHING`,
		clusterUUID, alias, "src-"+alias)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_savings_cents, updated_at)
		VALUES ($1, $2, 'ns1', 'w1', 'Deployment', 'c1', 'medium', 'cost', false, '{}', $3, now())`,
		testutil.TestOrgID, clusterUUID, money.USDToCents(containerSavingsUSD))
	require.NoError(t, err)
}

func TestSavings_NonUSD_Currency_Propagates(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	app, _, pool := setupSavingsSummaryWithMockKoku(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clusterID := r.URL.Query().Get("cluster_id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(effectiveRatesResponse("EUR", clusterID)))
	}))

	seedSavingsSummaryCluster(t, context.Background(), pool, testutil.TestClusterUUID, "eur-cluster", 250.50)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary", nil)
	req.Header.Set("X-Rh-Identity", savingsSummaryIdentity(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "savings summary should succeed with mocked EUR effective_rates: %s", rec.Body.String())

	var summary api.FleetSavingsSummaryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &summary))

	assert.Equal(t, "EUR", summary.Currency,
		"fleet savings summary currency should propagate from Koku effective_rates, not default USD")
	assert.Equal(t, "250.50", summary.EstimatedMonthlySavings.Value,
		"estimated_monthly_savings.value holds savings regardless of currency label")
	assert.Equal(t, "EUR", summary.EstimatedMonthlySavings.Units)
	assert.InDelta(t, 250.50, summary.ByPlugin.Container, 0.01,
		"by_plugin.container should reflect persisted savings in the cost model currency")
}

func TestSavingsSummary_CurrencyMismatch_MultiCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	currencyByCluster := map[string]string{
		currencyClusterEUR: "EUR",
		currencyClusterUSD: "USD",
	}

	app, ctx, pool := setupSavingsSummaryWithMockKoku(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clusterID := r.URL.Query().Get("cluster_id")
		currency := currencyByCluster[clusterID]
		if currency == "" {
			currency = costdata.DefaultCurrency
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(effectiveRatesResponse(currency, clusterID)))
	}))

	seedSavingsSummaryCluster(t, ctx, pool, currencyClusterEUR, "cluster-eur", 100.0)
	seedSavingsSummaryCluster(t, ctx, pool, currencyClusterUSD, "cluster-usd", 200.0)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary", nil)
	req.Header.Set("X-Rh-Identity", savingsSummaryIdentity(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var summary api.FleetSavingsSummaryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &summary))

	require.NotEmpty(t, summary.ByCluster)
	firstCluster := summary.ByCluster[0].ClusterUUID
	expectedCurrency := currencyByCluster[firstCluster]
	assert.Equal(t, expectedCurrency, summary.Currency,
		"multi-cluster fleet summary should pick the first cluster's currency when clusters disagree (no crash)")
	assert.Equal(t, "300.00", summary.EstimatedMonthlySavings.Value,
		"savings amounts should still aggregate numerically across mixed-currency clusters")
}

func TestSavings_CurrencyFromCostData_PassedToNodeSavings(t *testing.T) {
	t.Parallel()

	cd := &costdata.ClusterCostData{
		Currency: "GBP",
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour":  {Infrastructure: 0, Supplementary: 0.01},
			"memory_gb_usage_per_hour": {Infrastructure: 0, Supplementary: 0.02},
			"node_cost_per_month":      {Infrastructure: 500, Supplementary: 0},
		},
	}

	recs := []engine.NodeRec{{
		Node:               "worker-gbp",
		CurrentCPUMC:       8,
		RecommendedCPUMC:   4,
		CurrentMemKiB:      32,
		RecommendedMemKiB:  16,
		NodeCountReduction: 1,
	}}
	engine.ApplyNodeSavings(recs, cd)

	assert.Equal(t, "GBP", costdata.ResolveCurrency(cd),
		"node savings output should carry the cluster currency from cost data")
	require.Greater(t, recs[0].EstimatedMonthlySavingsCents, int64(0),
		"savings amount should be computed when currency is GBP")
	assert.NotContains(t, recs[0].NotificationCodes, engine.NotifNoCostData)
}

func TestSavings_CurrencyFromCostData_PassedToPVCSavings(t *testing.T) {
	t.Parallel()

	recommended := int64(10 * 1024 * 1024 * 1024)
	recs := []engine.PVCRec{{
		Namespace:        "apps",
		PVC:              "data-vol",
		RequestBytes:     100 * 1024 * 1024 * 1024,
		RecommendedBytes: &recommended,
	}}
	cd := &costdata.ClusterCostData{
		Currency: "GBP",
		ConfiguredRates: map[string]costdata.RatePair{
			"storage_gb_request_per_month": {Infrastructure: 0, Supplementary: 0.10},
		},
	}

	engine.ApplyPVCSavings(recs, cd)

	assert.Equal(t, "GBP", costdata.ResolveCurrency(cd),
		"PVC savings output should carry the cluster currency from cost data")
	require.InDelta(t, 9.0, money.CentsToUSD(recs[0].EstimatedMonthlySavingsCents), 0.01)
}

func TestSavings_NoCostData_DefaultsUSD(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -30)

	t.Run("NilCostDataProvider", func(t *testing.T) {
		provider := &costdata.NilCostDataProvider{}
		cd, err := provider.GetEffectiveRates(ctx, "1234567", testutil.TestClusterUUID, start, now)
		require.NoError(t, err)
		assert.Equal(t, "USD", costdata.ResolveCurrency(cd))
	})

	t.Run("HTTPEffectiveRatesUnavailable", func(t *testing.T) {
		if testing.Short() {
			t.Skip("requires PostgreSQL")
		}
		app, _, pool := setupSavingsSummaryWithMockKoku(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		seedSavingsSummaryCluster(t, context.Background(), pool, testutil.TestClusterUUID, "usd-default", 100.0)

		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary", nil)
		req.Header.Set("X-Rh-Identity", savingsSummaryIdentity(testutil.TestOrgID))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var summary api.FleetSavingsSummaryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &summary))
		assert.Equal(t, "USD", summary.Currency,
			"when effective_rates is unavailable, fleet savings summary currency should default to USD")
	})

	t.Run("EmptyCurrencyInResponse", func(t *testing.T) {
		if testing.Short() {
			t.Skip("requires PostgreSQL")
		}
		app, _, pool := setupSavingsSummaryWithMockKoku(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clusterID := r.URL.Query().Get("cluster_id")
			body := effectiveRatesResponse("", clusterID)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		seedSavingsSummaryCluster(t, context.Background(), pool, testutil.TestClusterUUID, "empty-currency", 50.0)

		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary", nil)
		req.Header.Set("X-Rh-Identity", savingsSummaryIdentity(testutil.TestOrgID))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var summary api.FleetSavingsSummaryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &summary))
		assert.Equal(t, "USD", summary.Currency,
			"empty currency in effective_rates response should fall back to USD")
	})
}
