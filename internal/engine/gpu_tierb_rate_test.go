package engine_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #942: the Tier-B GPU rate. When no gpu_cost_per_month is configured, GPU
// time-slicing/MIG savings fall back to a cluster-level rate derived from
// observed spend: (Σ namespace cost_model_gpu_cost) ÷ (cluster GPU-count-months
// from the node digests). GPU money never prices CPU/memory savings (#905);
// this rate prices only the GPU path.

const tierBTestMicroCentsPerDollar = 100_000_000

// seedTierBAccountCluster ensures the rh_accounts + clusters rows the
// digests reference (the sibling GPU integration test's pattern).
func seedTierBAccountCluster(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, orgID)
	require.NoError(t, err)
	var tenantID int
	err = pool.QueryRow(ctx, `SELECT id FROM rh_accounts WHERE org_id = $1`, orgID).Scan(&tenantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES ($1, $2, $3, 'src-tierb', now()) ON CONFLICT DO NOTHING`,
		tenantID, clusterUUID, "tierb-"+orgID)
	require.NoError(t, err)
}

// ensureTierBNodeDigestPartitions creates the monthly partitions the node
// digest inserts need (daily_node_digests is RANGE-partitioned on bucket_date;
// the sibling integration test's pattern).
func ensureTierBNodeDigestPartitions(t *testing.T, pool *pgxpool.Pool, start, end time.Time) {
	t.Helper()
	ctx := context.Background()
	firstMonth := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	lastMonth := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)
	for m := firstMonth; !m.After(lastMonth); m = m.AddDate(0, 1, 0) {
		monthEnd := m.AddDate(0, 1, 0)
		partName := fmt.Sprintf("daily_node_digests_%s", m.Format("200601"))
		_, err := pool.Exec(ctx, fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_node_digests FOR VALUES FROM ('%s') TO ('%s')`,
			partName, m.Format("2006-01-02"), monthEnd.Format("2006-01-02")))
		require.NoError(t, err)
	}
}

func TestGPUMonthlyRate_TierBFallbackAndPrecedence(t *testing.T) {
	configured := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"gpu_cost_per_month": {Infrastructure: 111_000_000_000},
		},
		TierBGPUMonthlyRateMicroCents: 222_000_000_000,
	}
	assert.Equal(t, int64(111_000_000_000), engine.GPUMonthlyRate(configured),
		"the configured rate (Tier A) always wins when present")

	tierBOnly := &costdata.ClusterCostData{
		TierBGPUMonthlyRateMicroCents: 222_000_000_000,
	}
	assert.Equal(t, int64(222_000_000_000), engine.GPUMonthlyRate(tierBOnly),
		"the Tier-B derived rate applies when no rate is configured")

	assert.Equal(t, int64(0), engine.GPUMonthlyRate(&costdata.ClusterCostData{}),
		"neither tier: no rate — savings stay zero (current behavior)")
	assert.Equal(t, int64(0), engine.GPUMonthlyRate(nil), "nil cost data: no rate")
}

func TestDeriveTierBGPURate(t *testing.T) {
	cd := &costdata.ClusterCostData{
		Namespaces: map[string]costdata.NamespaceCosts{
			// Two namespaces carrying GPU spend: $1,000 each.
			"ml-team":  {CostModelGPUCost: 1000 * tierBTestMicroCentsPerDollar},
			"infra-ns": {CostModelGPUCost: 1000 * tierBTestMicroCentsPerDollar},
		},
	}

	// 2 GPUs held for the whole month → 2 GPU-count-months.
	// Rate = $2,000 ÷ 2 GPUs = $1,000 per GPU per month.
	cd.DeriveTierBGPURate(2.0)
	assert.Equal(t, int64(1000*tierBTestMicroCentsPerDollar), cd.TierBGPUMonthlyRateMicroCents,
		"cluster GPU spend ÷ GPU-count-months is the per-GPU-month rate")

	t.Run("guards leave no rate (current behavior)", func(t *testing.T) {
		zeroMonths := &costdata.ClusterCostData{
			Namespaces: map[string]costdata.NamespaceCosts{
				"ml-team": {CostModelGPUCost: 1000 * tierBTestMicroCentsPerDollar},
			},
		}
		zeroMonths.DeriveTierBGPURate(0)
		assert.Equal(t, int64(0), zeroMonths.TierBGPUMonthlyRateMicroCents,
			"a cluster with no GPU inventory gets no rate (÷0 guarded)")

		zeroSpend := &costdata.ClusterCostData{Namespaces: map[string]costdata.NamespaceCosts{
			"ml-team": {CostModelGPUCost: 0},
		}}
		zeroSpend.DeriveTierBGPURate(2.0)
		assert.Equal(t, int64(0), zeroSpend.TierBGPUMonthlyRateMicroCents,
			"a cluster with no GPU spend gets no rate")
	})

	t.Run("on-cloud no-double-count: infrastructure spend never enters the numerator", func(t *testing.T) {
		// On OCP-on-cloud the cloud's GPU instance spend rides
		// infrastructure_raw_cost into the CPU/memory infra numerator; the
		// GPU rate's numerator is cost_model_gpu_cost ONLY — counting both
		// would double-count GPU money.
		cloudNS := &costdata.ClusterCostData{
			Namespaces: map[string]costdata.NamespaceCosts{
				"ml-team": {
					CostModelGPUCost: 500 * tierBTestMicroCentsPerDollar,
					InfraCost:        50_000 * tierBTestMicroCentsPerDollar, // cloud bill incl. GPU instances
				},
			},
		}
		cloudNS.DeriveTierBGPURate(1.0)
		assert.Equal(t, int64(500*tierBTestMicroCentsPerDollar), cloudNS.TierBGPUMonthlyRateMicroCents,
			"the rate reflects only the explicit GPU charge, never infrastructure spend")
	})
}

func TestClusterGPUCountMonths(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-tierb-gpu-months"
	clusterUUID := testutil.TestClusterUUID
	start := testutil.RecentStart()

	seedTierBAccountCluster(t, pool, orgID, clusterUUID)
	ensureTierBNodeDigestPartitions(t, pool, start, start.AddDate(0, 0, 29))

	// One node with 2 GPUs reporting for 30 days: 2 × 30 GPU-days = 2
	// GPU-count-months.
	for i := 0; i < 30; i++ {
		_, err := pool.Exec(ctx, `
			INSERT INTO daily_node_digests (
				bucket_date, org_id, cluster_uuid, node, schedule_type,
				cpu_usage_p50_mc, cpu_usage_p95_mc, mem_usage_p50_kib, mem_usage_p95_kib,
				max_cpu_allocatable_mc, max_mem_allocatable_kib, max_cpu_requests_mc, max_mem_requests_kib,
				max_pod_count, sample_count, node_gpu_count
			) VALUES ($1, $2, $3, 'gpu-node-a', 'all_hours',
				1000, 2000, 1000, 2000, 4000, 8000, 1000, 2000, 10, 24, 2)
			ON CONFLICT DO NOTHING`,
			start.AddDate(0, 0, i), orgID, clusterUUID)
		require.NoError(t, err)
	}

	months, err := engine.ClusterGPUCountMonths(ctx, pool, orgID, clusterUUID, start, start.AddDate(0, 0, 29))
	require.NoError(t, err)
	assert.InDelta(t, 2.0, months, 0.001,
		"2 GPUs × 30 days ÷ 30 = 2 GPU-count-months (all_hours schedule only)")

	// A cluster with no node digests: zero months (the ÷0 guard upstream).
	empty, err := engine.ClusterGPUCountMonths(ctx, pool, orgID, "00000000-0000-4000-8000-000000009999", start, start.AddDate(0, 0, 29))
	require.NoError(t, err)
	assert.Equal(t, 0.0, empty)
}

func TestGPUTimeslicingTierBFallback_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := "org-gpu-ts-tierb"
	clusterUUID := testutil.TestClusterUUID
	nodeName := "gpu-ts-tierb-node"
	start := testutil.RecentStart()
	terms := engine.DefaultTermsForPlugin("gpu")

	seedTierBAccountCluster(t, pool, orgID, clusterUUID)

	containers := []struct {
		ns, wl, cn string
		smAvg      float64
	}{
		{"ml-team", "training-a", "gpu-worker-a", 0.12},
		{"ml-team", "training-b", "gpu-worker-b", 0.08},
	}
	for _, c := range containers {
		testutil.SeedGPURecommendationSet(t, pool, orgID, clusterUUID, c.ns, c.wl, c.cn, "medium")
		for i := 0; i < 7; i++ {
			testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
				OrgID:         orgID,
				IntervalStart: start.AddDate(0, 0, i),
				ClusterUUID:   clusterUUID,
				Namespace:     c.ns,
				Workload:      c.wl,
				WorkloadType:  "deployment",
				ContainerName: c.cn,
				GPUModelName:  "NVIDIA T4",
				NodeName:      nodeName,
				FBUsageMinMiB: 500, FBUsageMaxMiB: 2000, FBUsageAvgMiB: 1000,
				TensorPipeActiveMin: 0.02, TensorPipeActiveMax: 0.10, TensorPipeActiveAvg: 0.05,
				DRAMActiveMin: 0.03, DRAMActiveMax: 0.13, DRAMActiveAvg: 0.05,
				SMActiveMin: c.smAvg - 0.03, SMActiveMax: c.smAvg + 0.05, SMActiveAvg: c.smAvg,
			})
		}
	}
	// The GPU inventory the denominator needs: one node, 2 GPUs, 30 days.
	ensureTierBNodeDigestPartitions(t, pool, start, start.AddDate(0, 0, 29))
	for i := 0; i < 30; i++ {
		_, err := pool.Exec(ctx, `
			INSERT INTO daily_node_digests (
				bucket_date, org_id, cluster_uuid, node, schedule_type,
				cpu_usage_p50_mc, cpu_usage_p95_mc, mem_usage_p50_kib, mem_usage_p95_kib,
				max_cpu_allocatable_mc, max_mem_allocatable_kib, max_cpu_requests_mc, max_mem_requests_kib,
				max_pod_count, sample_count, node_gpu_count
			) VALUES ($1, $2, $3, $4, 'all_hours',
				1000, 2000, 1000, 2000, 4000, 8000, 1000, 2000, 10, 24, 2)
			ON CONFLICT DO NOTHING`,
			start.AddDate(0, 0, i), orgID, clusterUUID, nodeName)
		require.NoError(t, err)
	}

	require.NoError(t, engine.MarkContainersWithGPU(ctx, pool, orgID, clusterUUID))
	require.NoError(t, engine.StoreGPUClassifications(ctx, pool, orgID, clusterUUID, terms, nil))

	// Tier-B only: namespace GPU spend ($2,000 total), no configured rates.
	costData := &costdata.ClusterCostData{
		Currency: "EUR",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ml-team":  {CostModelGPUCost: 1000 * tierBTestMicroCentsPerDollar},
			"other-ns": {CostModelGPUCost: 1000 * tierBTestMicroCentsPerDollar},
		},
	}
	end := start.AddDate(0, 0, 29)
	require.NoError(t, engine.EnsureTierBGPURate(ctx, pool, orgID, clusterUUID, start, end, costData))
	require.Equal(t, int64(1000*tierBTestMicroCentsPerDollar), costData.TierBGPUMonthlyRateMicroCents,
		"$2,000 spend ÷ 2 GPU-count-months = $1,000 per GPU-month")

	require.NoError(t, engine.ComputeAndPersistNodeGPUTimeSlicingRecs(
		ctx, pool, orgID, clusterUUID, terms, costData,
	))

	var savingsCents *int64
	err := pool.QueryRow(ctx, `
		SELECT estimated_savings_cents FROM node_gpu_timeslicing_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2 AND node_name = $3
		ORDER BY term LIMIT 1`, orgID, clusterUUID, nodeName).Scan(&savingsCents)
	require.NoError(t, err, "the timeslicing rec must persist")
	require.NotNil(t, savingsCents, "the Tier-B fallback must price the rec — pre-#942 this was NULL/zero")
	assert.Greater(t, *savingsCents, int64(0), "Tier-B GPU savings are non-zero")
}
