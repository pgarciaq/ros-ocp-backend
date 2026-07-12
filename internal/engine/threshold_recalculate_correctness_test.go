package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func ensureDailyNodeDigestPartitions(t *testing.T, pool *pgxpool.Pool, start time.Time, days int) {
	t.Helper()
	ctx := context.Background()
	if days < 1 {
		return
	}
	lastDate := start.AddDate(0, 0, days-1)
	firstMonth := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	lastMonth := time.Date(lastDate.Year(), lastDate.Month(), 1, 0, 0, 0, 0, time.UTC)
	for m := firstMonth; !m.After(lastMonth); m = m.AddDate(0, 1, 0) {
		monthEnd := m.AddDate(0, 1, 0)
		partName := fmt.Sprintf("daily_node_digests_%s", m.Format("200601"))
		_, err := pool.Exec(ctx, fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_node_digests FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			m.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		))
		require.NoError(t, err, "create partition %s", partName)
	}
}

func setupRecalcCorrectnessTest(t *testing.T) (*pgxpool.Pool, context.Context, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := fmt.Sprintf("org-recalc-correctness-%s", t.Name())

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (9002, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (9002, $1, 'recalc-test', 'src-recalc', NOW()) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID)
	require.NoError(t, err)

	return pool, ctx, orgID
}

func seedPercentileDigestSeries(t *testing.T, pool *pgxpool.Pool, orgID string, days int) {
	t.Helper()
	for i := 0; i < days; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:       testutil.RecentStart().AddDate(0, 0, i),
			OrgID:            orgID,
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        testutil.TestNamespace,
			Workload:         testutil.TestWorkload,
			WorkloadType:     testutil.TestWorkloadType,
			ContainerName:    testutil.TestContainer,
			CPURequestP50MC:  5000,
			CPURequestP95MC:  5500,
			CPUUsageP50MC:    2000,
			CPUUsageP60MC:    3000,
			CPUUsageP95MC:    4000,
			CPUUsageP98MC:    4500,
			CPUUsageP99MC:    4600,
			CPUUsageMaxMC:    5000,
			MemRequestP50KiB: 524288,
			MemRequestP95KiB: 550000,
			MemUsageP50KiB:   400000,
			MemUsageP95KiB:   500000,
			MemUsageP98KiB:   520000,
			MemUsageMaxKiB:   550000,
			SampleCount:      96,
		})
	}
}

func costEngineCPUFromRecs(recs []ContainerRec, term string) int64 {
	for _, r := range recs {
		if r.Term == term && r.Engine == "cost" {
			return r.RecCPURequestMC
		}
	}
	return 0
}

func runContainerRecs(t *testing.T, pool *pgxpool.Pool, orgID string) []ContainerRec {
	t.Helper()
	ctx := context.Background()
	start := testutil.RecentStart()
	end := start.AddDate(0, 0, 6)
	recs, err := RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, start, end, OOMConfig{})
	require.NoError(t, err)
	return recs
}

func TestRecalculation_ContainerPercentileChange_AffectsOutput(t *testing.T) {
	pool, ctx, orgID := setupRecalcCorrectnessTest(t)
	seedPercentileDigestSeries(t, pool, orgID, 7)

	require.NoError(t, UpdateThresholdSettings(ctx, pool, orgID, "container",
		json.RawMessage(`{"cpu_cost_percentile": 0.98}`)))

	recsHigh := runContainerRecs(t, pool, orgID)
	cpuP98 := costEngineCPUFromRecs(recsHigh, "short")
	require.True(t, cpuP98 >= 4000,
		"cost engine at P98 should recommend CPU near 4500mc (p98 usage), got %d", cpuP98)

	require.NoError(t, UpdateThresholdSettings(ctx, pool, orgID, "container",
		json.RawMessage(`{"cpu_cost_percentile": 0.50}`)))
	InvalidateThresholdCache(orgID, "container")

	recsLow := runContainerRecs(t, pool, orgID)
	cpuP50 := costEngineCPUFromRecs(recsLow, "short")
	require.True(t, cpuP50 >= 1500 && cpuP50 <= 2500,
		"cost engine at P50 should recommend CPU near 2000mc (p50 usage), got %d", cpuP50)
	assert.True(t, cpuP50 < cpuP98-500,
		"lowering cpu_cost_percentile from P98 to P50 should significantly reduce recommended CPU (got p98=%d, p50=%d)", cpuP98, cpuP50)
}

func seedNodesAtUtilization(t *testing.T, pool *pgxpool.Pool, orgID string, nodeNames []string, utilPct float64, days int) {
	t.Helper()
	ctx := context.Background()
	start := testutil.RecentStart()
	ensureDailyNodeDigestPartitions(t, pool, start, days)
	allocCPU := int64(10000)
	usageP95 := int64(float64(allocCPU) * utilPct)

	for _, nodeName := range nodeNames {
		for i := 0; i < days; i++ {
			date := start.AddDate(0, 0, i)
			_, err := pool.Exec(ctx, `
				INSERT INTO daily_node_digests (
					bucket_date, org_id, cluster_uuid, node,
					cpu_usage_p50_mc, cpu_usage_p95_mc,
					mem_usage_p50_kib, mem_usage_p95_kib,
					max_cpu_allocatable_mc, max_mem_allocatable_kib,
					max_cpu_requests_mc, max_mem_requests_kib,
					max_pod_count, sample_count
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
				ON CONFLICT (org_id, cluster_uuid, node, bucket_date) DO UPDATE SET
					cpu_usage_p95_mc = EXCLUDED.cpu_usage_p95_mc`,
				date, orgID, testutil.TestClusterUUID, nodeName,
				usageP95/2, usageP95,
				4000000, 8000000,
				allocCPU, 33554432,
				2000, 8388608,
				5, 24,
			)
			require.NoError(t, err)
		}
	}
}

func seedNodeAtUtilization(t *testing.T, pool *pgxpool.Pool, orgID, nodeName string, utilPct float64, days int) {
	t.Helper()
	seedNodesAtUtilization(t, pool, orgID, []string{nodeName}, utilPct, days)
}

func fleetNodesNeeded(recs []NodeRec) int {
	if len(recs) == 0 || recs[0].CurrentCPUMC <= 0 {
		return 0
	}
	nodeCapacity := float64(recs[0].CurrentCPUMC) / 1000.0
	var totalRecommended float64
	for _, r := range recs {
		totalRecommended += float64(r.RecommendedCPUMC) / 1000.0
	}
	return int(math.Ceil(totalRecommended / nodeCapacity))
}

func fleetNodeCountReduction(recs []NodeRec) int {
	total := 0
	for _, r := range recs {
		total += r.NodeCountReduction
	}
	return total
}

func nodeCostRecs(t *testing.T, pool *pgxpool.Pool, orgID string, nodeNames ...string) []NodeRec {
	t.Helper()
	ctx := context.Background()
	start := testutil.RecentStart()
	end := start.AddDate(0, 0, 7)

	digests, err := QueryNodeDigests(ctx, pool, orgID, testutil.TestClusterUUID, start, end.AddDate(0, 0, 1))
	require.NoError(t, err)

	nodeSettings, err := ResolveNodeThresholdSettings(ctx, pool, orgID)
	require.NoError(t, err)
	cfg := NodeRecConfigFromThresholds(nodeSettings)

	terms := []TermConfig{{Name: "medium", WindowDays: 30, MinDataDays: 3}}
	recs := RecommendNodes(digests, cfg, nodeSettings, terms)

	want := make(map[string]struct{}, len(nodeNames))
	for _, name := range nodeNames {
		want[name] = struct{}{}
	}

	var matched []NodeRec
	for _, r := range recs {
		if r.Engine != "cost" || r.Term != "medium" {
			continue
		}
		if _, ok := want[r.Node]; ok {
			matched = append(matched, r)
		}
	}
	require.Len(t, matched, len(nodeNames), "expected cost/medium recommendations for all nodes")
	return matched
}

func nodeCostRec(t *testing.T, pool *pgxpool.Pool, orgID, nodeName string) NodeRec {
	t.Helper()
	recs := nodeCostRecs(t, pool, orgID, nodeName)
	return recs[0]
}

func TestRecalculation_NodeTargetUtilization_AffectsConsolidation(t *testing.T) {
	pool, ctx, orgID := setupRecalcCorrectnessTest(t)
	nodeNames := []string{"node-a", "node-b", "node-c", "node-d"}
	seedNodesAtUtilization(t, pool, orgID, nodeNames, 0.40, 7)

	require.NoError(t, UpdateThresholdSettings(ctx, pool, orgID, "node",
		json.RawMessage(`{"cost_target_utilization": 0.80, "underutil_threshold": 0.50}`)))
	InvalidateThresholdCache(orgID, "node")

	recsHighTarget := nodeCostRecs(t, pool, orgID, nodeNames...)
	assert.Equal(t, 2, fleetNodeCountReduction(recsHighTarget),
		"four underutilized nodes at target 0.80 should fleet-consolidate by two nodes")
	assert.LessOrEqual(t, fleetNodesNeeded(recsHighTarget), 2,
		"40%% utilization on 4 nodes at target 0.80 should fit on ~2 right-sized nodes")

	require.NoError(t, UpdateThresholdSettings(ctx, pool, orgID, "node",
		json.RawMessage(`{"cost_target_utilization": 0.40}`)))
	InvalidateThresholdCache(orgID, "node")

	recsLowTarget := nodeCostRecs(t, pool, orgID, nodeNames...)
	assert.Greater(t, fleetNodesNeeded(recsLowTarget), fleetNodesNeeded(recsHighTarget),
		"lowering cost_target_utilization should increase fleet capacity needs and reduce consolidation headroom")
	assert.Equal(t, 4, fleetNodesNeeded(recsLowTarget),
		"40%% utilization on 4 nodes at target 0.40 should require all 4 nodes")
	assert.NotEqual(t, fleetNodesNeeded(recsHighTarget), fleetNodesNeeded(recsLowTarget),
		"node consolidation potential must change when target utilization changes")
}

func seedGPUAtUtilization(t *testing.T, pool *pgxpool.Pool, orgID string, smAvg float64, days int) {
	t.Helper()
	start := testutil.RecentStart()
	for i := 0; i < days; i++ {
		testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
			IntervalStart:       start.AddDate(0, 0, i),
			ClusterUUID:         testutil.TestClusterUUID,
			Namespace:           "gpu-ns",
			Workload:            "gpu-wl",
			WorkloadType:        "deployment",
			ContainerName:       "gpu-ctr",
			GPUModelName:        "NVIDIA A100",
			NodeName:            "gpu-node-1",
			SMActiveAvg:         smAvg,
			TensorPipeActiveAvg: 0.05,
			DRAMActiveAvg:       0.10,
			FBUsageAvgMiB:       2048,
		})
	}
}

func gpuClassification(t *testing.T, pool *pgxpool.Pool, orgID string) GPUClassification {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -7)
	terms := []TermConfig{{Name: "short", WindowDays: 7, MinDataDays: 3}}
	recs, _, _, err := QueryGPURecommendations(ctx, pool, orgID, testutil.TestClusterUUID, start, now, terms, nil)
	require.NoError(t, err)
	list := recs[GPUContainerKey{Namespace: "gpu-ns", Workload: "gpu-wl", ContainerName: "gpu-ctr"}]
	require.NotEmpty(t, list)
	return list[0].Classification
}

func TestRecalculation_GPUIdleThreshold_AffectsClassification(t *testing.T) {
	pool, ctx, orgID := setupRecalcCorrectnessTest(t)
	seedGPUAtUtilization(t, pool, orgID, 0.08, 7)

	require.NoError(t, UpdateThresholdSettings(ctx, pool, orgID, "gpu",
		json.RawMessage(`{"idle_threshold": 0.10}`)))
	InvalidateThresholdCache(orgID, "gpu")

	assert.Equal(t, GPUClassIdle, gpuClassification(t, pool, orgID),
		"8%% SM utilization should be classified idle when idle_threshold is 0.10")

	require.NoError(t, UpdateThresholdSettings(ctx, pool, orgID, "gpu",
		json.RawMessage(`{"idle_threshold": 0.05}`)))
	InvalidateThresholdCache(orgID, "gpu")

	classification := gpuClassification(t, pool, orgID)
	assert.NotEqual(t, GPUClassIdle, classification,
		"8%% SM utilization should no longer be idle when idle_threshold is lowered to 0.05")
	assert.Equal(t, GPUClassUnderutilized, classification,
		"8%% utilization with 0.05 idle threshold should classify as underutilized")
}
