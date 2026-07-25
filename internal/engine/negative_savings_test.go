package engine

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func nodeCostDataForNegativeTests() *costdata.ClusterCostData {
	return &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour":  {Infrastructure: 0, Supplementary: 0.01},
			"memory_gb_usage_per_hour": {Infrastructure: 0, Supplementary: 0.02},
		},
	}
}

const negTestGibKiB = 1024 * 1024

// TestNodeSavings_Negative_WhenScaleUpNeeded covers overloaded nodes (95% util vs 80% target)
// where the recommendation is to add capacity — savings should be negative.
func TestNodeSavings_Negative_WhenScaleUpNeeded(t *testing.T) {
	recs := []NodeRec{
		{
			Node:              "overloaded-worker",
			CurrentCPUMC:      4000,
			RecommendedCPUMC:  8000,
			CurrentMemKiB:     16 * negTestGibKiB,
			RecommendedMemKiB: 32 * negTestGibKiB,
			Category:          "overcommitted",
		},
	}
	ApplyNodeSavings(recs, nodeCostDataForNegativeTests())

	assert.Less(t, recs[0].EstimatedMonthlySavingsCents, int64(0),
		"scale-up recommendation should show negative savings (additional cost)")
}

// TestPVCSavings_Negative_WhenGrowthProjected covers PVCs near capacity with growth trend
// where the recommendation is a larger volume — savings should be negative.
func TestPVCSavings_Negative_WhenGrowthProjected(t *testing.T) {
	recommended := int64(250 * 1024 * 1024 * 1024)
	recs := []PVCRec{
		{
			Namespace:        "data-ns",
			PVC:              "near-full-vol",
			RequestBytes:     100 * 1024 * 1024 * 1024,
			RecommendedBytes: &recommended,
		},
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"storage_gb_request_per_month": {Infrastructure: 0, Supplementary: 0.10},
		},
	}
	ApplyPVCSavings(recs, cd)

	assert.Less(t, recs[0].EstimatedMonthlySavingsCents, int64(0),
		"PVC growth recommendation should show negative savings")
}

// TestContainerSavings_Negative_WhenUnderprovisioned covers containers hitting OOM with
// a scale-up recommendation — cost increases, savings should be negative.
func TestContainerSavings_Negative_WhenUnderprovisioned(t *testing.T) {
	recs := []ContainerRec{
		{
			Namespace:            "app-ns",
			CurrentCPURequestMC:  200,
			RecCPURequestMC:      500,
			CurrentMemRequestKiB: 512 * 1024,
			RecMemRequestKiB:     2 * 1024 * 1024,
			OOMCountSum:          12,
			PodCountAvg:          1,
		},
	}
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"app-ns": {
				CostModelCPUCost: 730.0,
				CostModelMemCost: 365.0,
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	ApplySavingsEstimates(recs, cd)

	require.NotNil(t, recs[0].EstimatedSavingsCents)
	assert.Less(t, *recs[0].EstimatedSavingsCents, int64(0),
		"under-provisioned container with scale-up recommendation should show negative savings")
}

func queryPluginSavingsTotals(ctx context.Context, pool *pgxpool.Pool, orgID string) (container, node, pvc, total float64, err error) {
	err = pool.QueryRow(ctx, `
		SELECT
			COALESCE((SELECT SUM(estimated_savings_cents)::float / 100.0 FROM recommendation_sets
				WHERE org_id = $1 AND term = 'medium' AND engine = 'cost' AND stale = false), 0),
			COALESCE((SELECT SUM(estimated_savings_cents)::float / 100.0 FROM node_recommendations
				WHERE org_id = $1 AND term = 'medium' AND engine = 'cost'), 0),
			COALESCE((SELECT SUM(estimated_savings_cents)::float / 100.0 FROM pvc_recommendation_sets
				WHERE org_id = $1 AND term = 'medium'), 0)`,
		orgID,
	).Scan(&container, &node, &pvc)
	total = container + node + pvc
	return
}

// TestSavingsSummary_AllowsNegativeTotal verifies fleet totals can be negative when
// scale-up costs exceed downsizing savings (not clamped to zero).
func TestSavingsSummary_AllowsNegativeTotal(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-negative-total"

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (9003, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (9003, $1, 'neg-total-cluster', 'src-neg-total', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	// Small container savings cannot offset a large node scale-up cost.
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_savings_cents, updated_at)
		VALUES ($1, $2, 'ns1', 'w1', 'Deployment', 'c1', 'medium', 'cost', false, '{}', 5000, now())`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, notification_codes, estimated_savings_cents, updated_at)
		VALUES ($1, $2, 'worker-overloaded', 'medium', 'cost', '{}', -50000, now())`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, _, _, total, err := queryPluginSavingsTotals(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Less(t, total, 0.0, "fleet total savings should be negative when scale-up dominates")
	assert.InDelta(t, -450.0, total, 0.01)
}

// TestSavingsSummary_ByPlugin_CanBeNegative verifies individual plugin aggregates
// preserve negative values (not clamped to zero).
func TestSavingsSummary_ByPlugin_CanBeNegative(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-negative-by-plugin"

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (9001, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (9001, $1, 'neg-cluster', 'src-neg', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_savings_cents, updated_at)
		VALUES ($1, $2, 'ns1', 'w1', 'Deployment', 'c1', 'medium', 'cost', false, '{}', 50000, now())`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, notification_codes, estimated_savings_cents, updated_at)
		VALUES ($1, $2, 'worker-overloaded', 'medium', 'cost', '{}', -20000, now())`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (org_id, cluster_uuid, namespace, persistentvolumeclaim, term, notification_codes, estimated_savings_cents, updated_at)
		VALUES ($1, $2, 'ns1', 'grow-vol', 'medium', '{}', -5000, now())`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	container, node, pvc, total, err := queryPluginSavingsTotals(ctx, pool, orgID)
	require.NoError(t, err)

	assert.InDelta(t, 500.0, container, 0.01)
	assert.Less(t, node, 0.0, "node plugin savings should be negative")
	assert.Less(t, pvc, 0.0, "pvc plugin savings should be negative")
	assert.InDelta(t, -200.0, node, 0.01)
	assert.InDelta(t, -50.0, pvc, 0.01)
	assert.InDelta(t, 250.0, total, 0.01)
}

// TestNodeSavings_Zero_WhenOptimallySized verifies no action needed when current
// capacity matches the recommendation — savings is explicitly zero.
func TestNodeSavings_Zero_WhenOptimallySized(t *testing.T) {
	recs := []NodeRec{
		{
			Node:               "right-sized-worker",
			CurrentCPUMC:       8000,
			RecommendedCPUMC:   8000,
			CurrentMemKiB:      32 * negTestGibKiB,
			RecommendedMemKiB:  32 * negTestGibKiB,
			NodeCountReduction: 0,
		},
	}
	ApplyNodeSavings(recs, nodeCostDataForNegativeTests())

	assert.Equal(t, int64(0), recs[0].EstimatedMonthlySavingsCents,
		"optimally sized node should have zero savings, not null or omitted")
}
