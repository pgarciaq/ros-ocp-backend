package costdata_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #798: end-to-end currency parity between the alias and UUID forms of
// filter[cluster] on the five #790 endpoints (nodes, pvcs, snapshots, quota,
// cluster-quota) plus the PVC and snapshot group-by surfaces. Pre-#798 the
// node list's display-currency path and both group-by endpoints passed the
// RAW filter value to fetchClusterCurrency — an alias failed the Koku cluster
// lookup and silently fell back to USD, mislabeling savings for non-USD orgs.
const (
	currencyParityCluster = "cccccccc-cccc-cccc-cccc-ccccccccccc4"
	currencyParityAlias   = "eur-parity"
	currencyParityCents   = int64(12345)
)

// currencyParityEndpoint describes one surface under test: its path, an
// optional group_by parameter (the group-by surfaces ride the same routes),
// and the JSON field carrying the money object.
type currencyParityEndpoint struct {
	name      string
	path      string
	groupBy   string
	moneyPath string // the money object's field name
	nested    bool   // nodes nest it under recommendation_terms/engines
}

func setupCurrencyParityApp(t *testing.T) (*echo.Echo, *pgxpool.Pool) {
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

	// Mock Koku: effective_rates serves EUR for the parity cluster's UUID
	// and 404 "no provider" for anything else — exactly like real koku, so a
	// raw-filter currency call with an alias value fails the lookup and
	// falls back to USD (the #798 bug class).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clusterID := r.URL.Query().Get("cluster_id")
		if clusterID != currencyParityCluster {
			w.WriteHeader(http.StatusNotFound)
			//nolint:gosec // G705: test fixture writes a constant template with a query param
			_, _ = fmt.Fprintf(w, `{"Error": "No provider found for cluster_id=%s."}`, clusterID)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(effectiveRatesResponse("EUR", clusterID)))
	}))
	t.Cleanup(srv.Close)
	enableKokuMockForSavings(t, srv.URL)

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})

	_, err := pool.Exec(ctx, `
		INSERT INTO rh_accounts (id, org_id) VALUES (1, $1)
		ON CONFLICT (id) DO UPDATE SET org_id = EXCLUDED.org_id`, testutil.TestOrgID)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/nodes", api.GetNodeUtilizationRecs)
	v1.GET("/recommendations/openshift/gpu/timeslicing", api.GetNodeRecommendations)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)
	v1.GET("/recommendations/openshift/snapshots", api.GetSnapshotRecommendations)
	v1.GET("/recommendations/openshift/quota", api.GetQuotaRecommendations)
	v1.GET("/recommendations/openshift/cluster-quota", api.GetClusterQuotaRecommendations)

	return app, pool
}

func seedCurrencyParityRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	seedSavingsSummaryCluster(t, ctx, pool, currencyParityCluster, currencyParityAlias, 0)

	_, err := pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine, notification_codes, estimated_savings_cents, updated_at
		) VALUES ($1, $2, 'worker-eur', 'medium', 'cost', '{}', $3, NOW())
		ON CONFLICT DO NOTHING`,
		testutil.TestOrgID, currencyParityCluster, currencyParityCents)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (
			org_id, cluster_uuid, namespace, persistentvolumeclaim, term,
			storageclass, recommendation_type, usage_ratio, capacity_bytes, usage_bytes_max,
			notification_codes, data_days, estimated_savings_cents, updated_at
		) VALUES ($1, $2, 'ns-eur', 'pvc-eur', 'medium', 'gp2', 'resize', 0.5, 10737418240, 1073741824, '{}', 14, $3, NOW())
		ON CONFLICT DO NOTHING`,
		testutil.TestOrgID, currencyParityCluster, currencyParityCents)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (
			org_id, cluster_uuid, namespace, snapshot_name, creation_timestamp,
			estimated_cost_cents, notification_codes, updated_at
		) VALUES ($1, $2, 'ns-eur', 'snap-eur', NOW(), $3, '{}', NOW())
		ON CONFLICT DO NOTHING`,
		testutil.TestOrgID, currencyParityCluster, currencyParityCents)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO quota_recommendation_sets (
			org_id, cluster_uuid, namespace, quota_id,
			cpu_request_hard_millicores, cpu_request_used_millicores,
			cpu_request_recommended_millicores,
			cpu_request_utilization_bp, recommendation_type, risk_level,
			estimated_savings_cents, notification_codes, last_observed_at
		) VALUES ($1, $2, 'ns-eur', 'quota-eur', 100000, 25000, 36000, 2500, 'tighten', 'low', $3, '{}', NOW())
		ON CONFLICT DO NOTHING`,
		testutil.TestOrgID, currencyParityCluster, currencyParityCents)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO cluster_quota_recommendation_sets (
			org_id, cluster_uuid, cluster_quota_name,
			cpu_request_hard, cpu_request_used, cpu_request_recommended,
			recommendation_type, risk_level, namespaces,
			utilization_cpu_request_percent,
			savings_cpu_cores_freed, savings_memory_bytes_freed,
			savings_storage_bytes_freed, savings_pods_freed,
			estimated_savings_cents, notification_codes
		) VALUES ($1, $2, 'cq-eur', 100000, 25000, 36000, 'resize', 'low', '{}', 25,
			2, 1073741824, 5368709120, 5, $3, '{}')
		ON CONFLICT DO NOTHING`,
		testutil.TestOrgID, currencyParityCluster, currencyParityCents)
	require.NoError(t, err)

	// A persisted GPU time-slicing rec routes the GPU node list through the
	// FromTable path, whose display currency comes from the raw filter
	// (nodeGPURecsDisplayCurrency) — the fourth #798 site.
	_, err = pool.Exec(ctx, `
		INSERT INTO node_gpu_timeslicing_recommendations (
			org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, confidence_level,
			candidate_count, impacted_count,
			candidate_containers, impacted_containers,
			notification_codes
		) VALUES ($1, $2, 'worker-eur', 'NVIDIA T4', 'medium',
			4, 0.75, 0.75, 1, 0,
			'[{"namespace":"ns-eur","workload":"w1","container":"c1","sm_active_avg":0.12,"classification":"underutilized"}]'::jsonb, '[]'::jsonb, '{}')
		ON CONFLICT (org_id, cluster_uuid, node_name, gpu_model, term) DO NOTHING`,
		testutil.TestOrgID, currencyParityCluster)
	require.NoError(t, err)
}

// TestClusterFilter_UnknownAliasIsEmpty pins the #790 unknown-alias contract
// on surfaces this change touched: an alias that matches no cluster returns
// 200 with count 0 — never 503, and never (post-#798) a silently unfiltered
// list. The GPU time-slicing and MIG entries normalize known aliases before
// the exact-UUID restrict; an unknown alias must still miss.
func TestClusterFilter_UnknownAliasIsEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	app, pool := setupCurrencyParityApp(t)
	seedCurrencyParityRows(t, pool)

	paths := []string{
		"/recommendations/openshift/gpu/timeslicing",
		"/recommendations/openshift/pvcs",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1"+path+"?filter[cluster]=no-such-alias", nil)
		req.Header.Set("X-Rh-Identity", savingsSummaryIdentity())
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "%s: body: %s", path, rec.Body.String())
		var parsed struct {
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &parsed))
		assert.Equal(t, 0, parsed.Meta.Count,
			"%s: an unknown alias must return an empty list, not an error and not unfiltered rows", path)
	}
}

// moneyFromRow extracts {value, units} from a data row by field name.
func moneyFromRow(t *testing.T, row map[string]interface{}, field string) map[string]interface{} {
	t.Helper()
	raw, ok := row[field]
	if !ok {
		return nil
	}
	m, _ := raw.(map[string]interface{})
	return m
}

// firstRowMoney extracts the first non-nil {value, units} money object from
// the response. Nodes nest it under recommendation_terms.<term>.
// recommendation_engines.<engine>; every other surface serves it as a
// top-level data-row field.
func firstRowMoney(t *testing.T, body []byte, field string, nested bool) map[string]interface{} {
	t.Helper()
	var parsed struct {
		Data []map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &parsed), "body: %s", body)
	for _, row := range parsed.Data {
		if !nested {
			if m := moneyFromRow(t, row, field); m != nil {
				return m
			}
			continue
		}
		terms, _ := row["recommendation_terms"].(map[string]interface{})
		for _, termData := range terms {
			engines, _ := termData.(map[string]interface{})["recommendation_engines"].(map[string]interface{})
			for _, engineData := range engines {
				if m := moneyFromRow(t, engineData.(map[string]interface{}), field); m != nil {
					return m
				}
			}
		}
	}
	return nil
}

func TestClusterFilterCurrencyParity_AliasVsUUID(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	app, pool := setupCurrencyParityApp(t)
	seedCurrencyParityRows(t, pool)

	endpoints := []currencyParityEndpoint{
		{name: "nodes list", path: "/recommendations/openshift/nodes", moneyPath: "estimated_monthly_savings", nested: true},
		{name: "gpu timeslicing nodes", path: "/recommendations/openshift/gpu/timeslicing", moneyPath: "estimated_monthly_savings"},
		{name: "pvcs list", path: "/recommendations/openshift/pvcs", moneyPath: "estimated_monthly_savings"},
		{name: "snapshots list", path: "/recommendations/openshift/snapshots", moneyPath: "estimated_monthly_cost"},
		{name: "quota list", path: "/recommendations/openshift/quota", moneyPath: "estimated_savings"},
		{name: "cluster-quota list", path: "/recommendations/openshift/cluster-quota", moneyPath: "estimated_savings"},
		{name: "pvc groups", path: "/recommendations/openshift/pvcs", groupBy: "namespace", moneyPath: "estimated_monthly_savings"},
		{name: "snapshot groups", path: "/recommendations/openshift/snapshots", groupBy: "cluster", moneyPath: "estimated_monthly_cost"},
	}

	getResponse := func(ep currencyParityEndpoint, clusterFilter string) (money map[string]interface{}, metaCurrency string) {
		url := fmt.Sprintf("/api/cost-management/v1%s?filter[cluster]=%s", ep.path, clusterFilter)
		if ep.groupBy != "" {
			url += "&group_by=" + ep.groupBy
		}
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-Rh-Identity", savingsSummaryIdentity())
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "%s (%s): body: %s", ep.name, clusterFilter, rec.Body.String())
		m := firstRowMoney(t, rec.Body.Bytes(), ep.moneyPath, ep.nested)

		var meta struct {
			Meta struct {
				Currency string `json:"currency"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &meta))
		require.True(t, m != nil || meta.Meta.Currency != "",
			"%s (%s): neither a money object nor meta currency in response: %s", ep.name, clusterFilter, rec.Body.String())
		return m, meta.Meta.Currency
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			aliasMoney, aliasMeta := getResponse(ep, currencyParityAlias)
			uuidMoney, uuidMeta := getResponse(ep, currencyParityCluster)

			if uuidMoney != nil {
				assert.Equal(t, "EUR", uuidMoney["units"],
					"%s: the UUID form must carry the cluster currency (EUR)", ep.name)
				assert.Equal(t, uuidMoney, aliasMoney,
					"%s: alias and UUID filter forms must return identical money objects — a mismatch is the raw-filter USD fallback (#798)", ep.name)
			}
			if uuidMeta != "" {
				assert.Equal(t, uuidMeta, aliasMeta,
					"%s: alias and UUID filter forms must return identical meta currency — a mismatch is the raw-filter USD fallback (#798)", ep.name)
			}
		})
	}
}
