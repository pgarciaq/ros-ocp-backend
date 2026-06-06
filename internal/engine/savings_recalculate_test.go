package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestNormalizeSavingsRecTypesForAPI(t *testing.T) {
	assert.Equal(t, []string{
		savingsRecTypeContainer, savingsRecTypeNode, savingsRecTypePVC,
		savingsRecTypeQuota, savingsRecTypeClusterQuota,
	}, NormalizeSavingsRecTypesForAPI(nil))
	assert.Equal(t, []string{savingsRecTypeNode}, NormalizeSavingsRecTypesForAPI([]string{"node", "NODE"}))
	assert.Equal(t, []string{savingsRecTypeQuota, savingsRecTypeClusterQuota},
		NormalizeSavingsRecTypesForAPI([]string{"quota", "cluster-quota"}))
}

func TestRecalculateSavingsForOrg_NodeUpdatesSavingsNotClassification(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "true")

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-savings-recalc-node"
	clusterUUID := "11111111-1111-1111-1111-111111111111"
	seedClustersForRecalcTest(t, pool, orgID, clusterUUID)

	const highNodeRate = 5000.0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"cluster_id": %q,
			"currency": "USD",
			"configured_rates": {
				"cpu_core_usage_per_hour": {"infrastructure": 0, "supplementary": 0.01},
				"memory_gb_usage_per_hour": {"infrastructure": 0, "supplementary": 0.02},
				"node_cost_per_month": {"infrastructure": %g, "supplementary": 0}
			},
			"namespace_aggregates": {}
		}`, clusterUUID, highNodeRate)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KOKU_MASU_URL", srv.URL)
	config.ResetForTest()
	costdata.ClearCostDataCacheForTest()
	t.Cleanup(costdata.ClearCostDataCacheForTest)

	bucketDate := testutil.BaseDate
	ensureDailyNodeDigestPartitions(t, pool, bucketDate, 1)
	_, err := pool.Exec(ctx, `
		INSERT INTO daily_node_digests (
			bucket_date, org_id, cluster_uuid, node,
			max_cpu_allocatable_mc, max_mem_allocatable_kib, max_cpu_requests_mc, max_mem_requests_kib,
			sample_count
		) VALUES ($1, $2, $3::uuid, 'worker-1', 8000, $4, 4000, $5, 1)
		ON CONFLICT (org_id, cluster_uuid, node, bucket_date) DO NOTHING`,
		bucketDate, orgID, clusterUUID, int64(32*1024*1024), int64(16*1024*1024))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			recommended_cpu_cores, recommended_memory_gib, node_count_reduction,
			estimated_monthly_savings_usd, notification_codes
		) VALUES ($1, $2::uuid, 'worker-1', 'medium', 'cost',
			0.10, 0.25, 0.15, 0.30,
			4.0, 16.0, 1, 0, '{}')`,
		orgID, clusterUUID)
	require.NoError(t, err)

	var savingsBefore int64
	err = pool.QueryRow(ctx, `
		SELECT estimated_monthly_savings_usd FROM node_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND node = 'worker-1'`,
		orgID, clusterUUID).Scan(&savingsBefore)
	require.NoError(t, err)
	assert.Equal(t, int64(0), savingsBefore)

	var cpuUtilP95 float32
	err = pool.QueryRow(ctx, `
		SELECT cpu_util_p95 FROM node_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND node = 'worker-1'`,
		orgID, clusterUUID).Scan(&cpuUtilP95)
	require.NoError(t, err)
	require.InDelta(t, 0.25, float64(cpuUtilP95), 0.001)

	RecalculateSavingsForOrg(ctx, pool, orgID, clusterUUID, []string{savingsRecTypeNode})

	var savingsAfter int64
	err = pool.QueryRow(ctx, `
		SELECT estimated_monthly_savings_usd FROM node_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND node = 'worker-1'`,
		orgID, clusterUUID).Scan(&savingsAfter)
	require.NoError(t, err)
	require.Greater(t, savingsAfter, int64(0))
	require.Greater(t, money.CentsToUSD(savingsAfter), 300.0,
		"savings should be recomputed from effective rates (was 0 before recalc)")

	err = pool.QueryRow(ctx, `
		SELECT cpu_util_p95 FROM node_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND node = 'worker-1'`,
		orgID, clusterUUID).Scan(&cpuUtilP95)
	require.NoError(t, err)
	require.InDelta(t, 0.25, float64(cpuUtilP95), 0.001, "classification fields must be unchanged")
}

func TestRecalculateSavingsForOrg_OnlyAffectedOrg(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	t.Setenv("KOKU_MASU_URL", "")

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	targetOrg := "org-savings-recalc-scope-a"
	otherOrg := "org-savings-recalc-scope-b"
	clusterA := "22222222-2222-2222-2222-222222222222"
	clusterB := "33333333-3333-3333-3333-333333333333"
	seedClustersForRecalcTest(t, pool, targetOrg, clusterA)
	seedClustersForRecalcTest(t, pool, otherOrg, clusterB)

	const targetCents int64 = 4242
	const otherCents int64 = 9999
	_, err := pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, estimated_monthly_savings_usd, notification_codes)
		VALUES ($1, $2::uuid, 'n1', 'medium', 'cost', $3, '{}'),
		       ($4, $5::uuid, 'n2', 'medium', 'cost', $6, '{}')`,
		targetOrg, clusterA, targetCents, otherOrg, clusterB, otherCents)
	require.NoError(t, err)

	restore := SetClusterSavingsRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, orgID, clusterUUID string, recTypes []string) error {
		recs := []NodeRec{{
			Node:                         "n1",
			Term:                         "medium",
			Engine:                       "cost",
			NodeCountReduction:           1,
			EstimatedMonthlySavingsCents: 1111,
		}}
		return updateNodeSavings(ctx, p, orgID, clusterUUID, recs)
	})
	defer restore()

	RecalculateSavingsForOrg(ctx, pool, targetOrg, "", []string{savingsRecTypeNode})

	var gotTarget, gotOther int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT estimated_monthly_savings_usd FROM node_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2::uuid`, targetOrg, clusterA).Scan(&gotTarget))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT estimated_monthly_savings_usd FROM node_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2::uuid`, otherOrg, clusterB).Scan(&gotOther))
	assert.Equal(t, int64(1111), gotTarget)
	assert.Equal(t, otherCents, gotOther)
}

func TestRecalculateSavingsForOrg_DoesNotInvokeThresholdEngine(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")

	pool := testutil.SetupTestDB(t)
	orgID := "org-savings-recalc-no-engine"
	clusterUUID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	seedClustersForRecalcTest(t, pool, orgID, clusterUUID)

	var thresholdEngineCalled bool
	restore := SetClusterRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID, recType string) error {
		thresholdEngineCalled = true
		return nil
	})
	defer restore()

	RecalculateSavingsForOrg(context.Background(), pool, orgID, clusterUUID, []string{savingsRecTypeNode})
	assert.False(t, thresholdEngineCalled, "threshold recalculation engine must not run during savings-only recalc")
}

func TestTriggerSavingsRecalculationAsync_RespectsKillSwitch(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "false")

	pool := testutil.SetupTestDB(t)
	triggered := false
	SetSavingsRecalcHookForTest(func(orgID string, recTypes []string) {
		triggered = true
	})
	defer ClearSavingsRecalcHookForTest()

	TriggerSavingsRecalculationAsync(pool, "org-x", "", nil)
	time.Sleep(50 * time.Millisecond)
	assert.False(t, triggered)
}

func TestApplyNodeSavings_RateChangeRecomputesValue(t *testing.T) {
	recs := []NodeRec{{
		CurrentCPUMC:       8000,
		RecommendedCPUMC:   4000,
		CurrentMemKiB:      32 * 1024 * 1024,
		RecommendedMemKiB:  16 * 1024 * 1024,
		NodeCountReduction: 1,
	}}
	lowRates := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"node_cost_per_month": {Infrastructure: 100, Supplementary: 0},
		},
	}
	highRates := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"node_cost_per_month": {Infrastructure: 5000, Supplementary: 0},
		},
	}
	ApplyNodeSavings(recs, lowRates)
	lowCents := recs[0].EstimatedMonthlySavingsCents
	ApplyNodeSavings(recs, highRates)
	highCents := recs[0].EstimatedMonthlySavingsCents
	require.Greater(t, highCents, lowCents)
}

func TestRecalculateSavingsForOrg_QuotaUpdatesSavingsNotClassification(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "true")

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-savings-recalc-quota"
	clusterUUID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	namespace := "quota-recalc-ns"
	seedClustersForRecalcTest(t, pool, orgID, clusterUUID)

	const highCPURate = 1.0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"cluster_id": %q,
			"currency": "USD",
			"configured_rates": {
				"cpu_core_usage_per_hour": {"infrastructure": 0, "supplementary": %g},
				"memory_gb_usage_per_hour": {"infrastructure": 0, "supplementary": 0.02},
				"storage_gb_usage_per_month": {"infrastructure": 0, "supplementary": 0.01}
			},
			"namespace_aggregates": {}
		}`, clusterUUID, highCPURate)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KOKU_MASU_URL", srv.URL)
	config.ResetForTest()
	costdata.ClearCostDataCacheForTest()
	t.Cleanup(costdata.ClearCostDataCacheForTest)

	const cpuFreedMC int64 = 64000
	_, err := pool.Exec(ctx, `
		INSERT INTO quota_recommendation_sets (
			org_id, cluster_uuid, namespace, quota_name,
			cpu_request_hard_millicores, cpu_request_used_millicores,
			cpu_request_recommended_millicores,
			cpu_freed_millicores, recommendation_type, risk_level,
			estimated_savings_cents, last_observed_at
		) VALUES ($1, $2::uuid, $3, 'team-budget', 100000, 25000, 36000,
			$4, 'tighten', 'low', 0, NOW())`,
		orgID, clusterUUID, namespace, cpuFreedMC)
	require.NoError(t, err)

	var savingsBefore int64
	err = pool.QueryRow(ctx, `
		SELECT estimated_savings_cents FROM quota_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3`,
		orgID, clusterUUID, namespace).Scan(&savingsBefore)
	require.NoError(t, err)
	assert.Equal(t, int64(0), savingsBefore)

	var recType string
	err = pool.QueryRow(ctx, `
		SELECT recommendation_type FROM quota_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3`,
		orgID, clusterUUID, namespace).Scan(&recType)
	require.NoError(t, err)
	require.Equal(t, QuotaRecTypeTighten, recType)

	RecalculateSavingsForOrg(ctx, pool, orgID, clusterUUID, []string{savingsRecTypeQuota})

	var savingsAfter int64
	err = pool.QueryRow(ctx, `
		SELECT estimated_savings_cents FROM quota_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3`,
		orgID, clusterUUID, namespace).Scan(&savingsAfter)
	require.NoError(t, err)
	require.Greater(t, savingsAfter, int64(0))
	require.Greater(t, money.CentsToUSD(savingsAfter), 400.0,
		"savings should be recomputed from effective rates (was 0 before recalc)")

	err = pool.QueryRow(ctx, `
		SELECT recommendation_type FROM quota_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3`,
		orgID, clusterUUID, namespace).Scan(&recType)
	require.NoError(t, err)
	require.Equal(t, QuotaRecTypeTighten, recType, "classification fields must be unchanged")
}

func TestRecalculateQuotaSavings_Unit(t *testing.T) {
	recs := []QuotaRec{{
		OrgID:                "org1",
		ClusterUUID:          "cluster1",
		Namespace:            "ns1",
		QuotaName:            "q1",
		RecommendationType:   QuotaRecTypeTighten,
		CapacityFreed:        QuotaCapacityFreed{CPUMillicores: 10000},
		EstimatedSavingsCents: 100,
	}}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour": {Infrastructure: 0, Supplementary: 0.5},
		},
	}
	ApplyQuotaSavings(recs, cd)
	require.Greater(t, recs[0].EstimatedSavingsCents, int64(100))
}

func TestRecalculateClusterQuotaSavings_Unit(t *testing.T) {
	recs := []ClusterQuotaRec{{
		OrgID:              "org1",
		ClusterUUID:        "cluster1",
		ClusterQuotaName:   "crq1",
		RecommendationType: QuotaRecTypeTighten,
		CapacityFreed:      QuotaCapacityFreed{CPUMillicores: 2000},
		EstimatedSavingsCents: 100,
	}}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour": {Infrastructure: 0, Supplementary: 1.0},
		},
	}
	ApplyClusterQuotaSavings(recs, cd)
	require.Greater(t, recs[0].EstimatedSavingsCents, int64(100))
}
