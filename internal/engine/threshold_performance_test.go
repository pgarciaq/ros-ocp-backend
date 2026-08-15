package engine

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// thresholdPerfPoolHandle returns the shared test DB pool so resolveThresholdCached
// exercises the cache path with a non-nil pool without starting another container.
func thresholdPerfPoolHandle(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	return testutil.SetupTestDB(tb)
}

func seedThresholdCacheForOrgs(orgCount int, recType string) []string {
	ClearThresholdSettingsCacheForTest()
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())

	defaults := DefaultContainerSizingThresholds()
	until := thresholdSettingsNow().Add(thresholdSettingsCacheTTL)

	orgIDs := make([]string, orgCount)
	thresholdSettingsMu.Lock()
	for i := 0; i < orgCount; i++ {
		orgID := fmt.Sprintf("org-threshold-perf-%d", i)
		orgIDs[i] = orgID
		key := thresholdSettingsCacheKey{orgID: orgID, recommendationType: recType}
		thresholdSettingsCache[key] = thresholdSettingsCacheEntry{value: defaults, until: until}
	}
	thresholdSettingsMu.Unlock()
	updateThresholdCacheEntriesGauge()
	return orgIDs
}

func p99Duration(samples []time.Duration) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := (len(sorted) * 99) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func resolveThresholdForOrg(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	_, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	return err
}

// BenchmarkThresholdResolution_SingleOrg measures cached threshold resolution for one org
// (target <1ms per resolution after warm-up).
func BenchmarkThresholdResolution_SingleOrg(b *testing.B) {
	pool := thresholdPerfPoolHandle(b)
	ctx := context.Background()
	orgIDs := seedThresholdCacheForOrgs(1, "container")
	b.Cleanup(ClearThresholdSettingsCacheForTest)

	if err := resolveThresholdForOrg(ctx, pool, orgIDs[0]); err != nil {
		b.Fatalf("warm-up resolve: %v", err)
	}

	latencies := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if err := resolveThresholdForOrg(ctx, pool, orgIDs[0]); err != nil {
			b.Fatalf("resolve: %v", err)
		}
		latencies = append(latencies, time.Since(start))
	}
	b.StopTimer()

	p99 := p99Duration(latencies)
	b.ReportMetric(float64(p99.Microseconds())/1000.0, "p99_ms/op")
	if p99 > time.Millisecond {
		b.Errorf("p99 latency %v exceeds 1ms threshold", p99)
	}
}

// BenchmarkThresholdResolution_50Orgs validates cached threshold resolution stays fast
// under load (p99 < 10ms per resolution after warm-up).
func BenchmarkThresholdResolution_50Orgs(b *testing.B) {
	const orgCount = 50
	pool := thresholdPerfPoolHandle(b)
	ctx := context.Background()
	orgIDs := seedThresholdCacheForOrgs(orgCount, "container")
	b.Cleanup(ClearThresholdSettingsCacheForTest)

	// Warm-up: populate CPU caches and verify all resolutions succeed.
	for _, orgID := range orgIDs {
		if err := resolveThresholdForOrg(ctx, pool, orgID); err != nil {
			b.Fatalf("warm-up resolve %s: %v", orgID, err)
		}
	}

	latencies := make([]time.Duration, 0, b.N*orgCount)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, orgID := range orgIDs {
			start := time.Now()
			if err := resolveThresholdForOrg(ctx, pool, orgID); err != nil {
				b.Fatalf("resolve %s: %v", orgID, err)
			}
			latencies = append(latencies, time.Since(start))
		}
	}
	b.StopTimer()

	perResolution := p99Duration(latencies)
	b.ReportMetric(float64(perResolution.Microseconds())/1000.0, "p99_ms/op")
	if perResolution > 10*time.Millisecond {
		b.Errorf("p99 latency %v exceeds 10ms threshold", perResolution)
	}
}

func benchContainerCostData() *costdata.ClusterCostData {
	return &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns-shared": {
				CostModelCPUCost: 730.0,
				CostModelMemCost: 365.0,
				InfraCost:        365.0,
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
}

func benchContainerRecs(count int) []ContainerRec {
	recs := make([]ContainerRec, count)
	for i := range recs {
		nsIdx := i % 50
		recs[i] = ContainerRec{
			Namespace:            fmt.Sprintf("ns-%d", nsIdx),
			CurrentCPURequestMC:  500 + int64(i%200),
			RecCPURequestMC:      200 + int64(i%100),
			CurrentMemRequestKiB: 2 * 1024 * 1024,
			RecMemRequestKiB:     1 * 1024 * 1024,
			PodCountAvg:          1 + int64(i%3),
		}
	}
	return recs
}

// BenchmarkSavingsCalculation_1000Containers measures container savings calculation
// for 1000 containers without DB I/O (in-memory cost data only).
func BenchmarkSavingsCalculation_1000Containers(b *testing.B) {
	const containerCount = 1000
	recs := benchContainerRecs(containerCount)
	cd := benchContainerCostData()

	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		ApplySavingsEstimates(recs, costdata.ClusterCostDataToRateCard(cd), 730)
	}
	elapsed := time.Since(start)
	if b.N > 0 {
		total := elapsed / time.Duration(b.N)
		b.ReportMetric(float64(total.Milliseconds()), "ms/iter")
		if total > time.Second {
			b.Errorf("total calculation time %v exceeds 1s threshold", total)
		}
	}
}

func benchNodeRecs(count int) []NodeRec {
	const gibKiB = 1024 * 1024
	recs := make([]NodeRec, count)
	for i := range recs {
		util := float32(0.3 + float64(i%70)/100.0)
		currentCPUmc := int64(8000 + (i%4)*1000)
		currentMemKiB := int64((32 + i%16) * gibKiB)
		scale := 0.5 + float64(util)
		recs[i] = NodeRec{
			Node:               fmt.Sprintf("worker-%d", i),
			CurrentCPUMC:       currentCPUmc,
			RecommendedCPUMC:   int64(float64(currentCPUmc) * scale),
			CurrentMemKiB:      currentMemKiB,
			RecommendedMemKiB:  int64(float64(currentMemKiB) * scale),
			NodeCountReduction: i % 2,
		}
	}
	return recs
}

func benchNodeCostData() *costdata.ClusterCostData {
	return &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour":  {Infrastructure: 0, Supplementary: 0.01},
			"memory_gb_usage_per_hour": {Infrastructure: 0, Supplementary: 0.02},
			"node_cost_per_month":      {Infrastructure: 1000, Supplementary: 0},
		},
	}
}

// BenchmarkNodeSavings_100Nodes calculates node savings for 100 nodes with varying utilization.
func BenchmarkNodeSavings_100Nodes(b *testing.B) {
	const nodeCount = 100
	recs := benchNodeRecs(nodeCount)
	cd := benchNodeCostData()

	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		ApplyNodeSavings(recs, cd, 730)
	}
	elapsed := time.Since(start)
	if b.N > 0 {
		total := elapsed / time.Duration(b.N)
		b.ReportMetric(float64(total.Milliseconds()), "ms/iter")
		if total > 500*time.Millisecond {
			b.Errorf("total calculation time %v exceeds 500ms threshold", total)
		}
	}
}

// TestThresholdResolution_ScalesLinearly verifies resolution time scales approximately
// linearly when resolving cached thresholds for increasing org counts.
func TestThresholdResolution_ScalesLinearly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping threshold scaling test (requires pool handle)")
	}
	pool := thresholdPerfPoolHandle(t)
	ctx := context.Background()

	const maxOrgs = 100
	orgIDs := seedThresholdCacheForOrgs(maxOrgs, "container")
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	for _, orgID := range orgIDs {
		if err := resolveThresholdForOrg(ctx, pool, orgID); err != nil {
			t.Fatalf("warm-up resolve %s: %v", orgID, err)
		}
	}

	counts := []int{1, 10, 50, 100}
	times := make(map[int]time.Duration, len(counts))

	const iterations = 2000
	for _, n := range counts {
		subset := orgIDs[:n]
		var total time.Duration
		for iter := 0; iter < iterations; iter++ {
			start := time.Now()
			for _, orgID := range subset {
				if err := resolveThresholdForOrg(ctx, pool, orgID); err != nil {
					t.Fatalf("resolve %s: %v", orgID, err)
				}
			}
			total += time.Since(start)
		}
		times[n] = total / iterations
		t.Logf("threshold resolution: %d orgs -> %v total (%v per org)",
			n, times[n], times[n]/time.Duration(n))
	}

	// Compare per-org average time so microsecond noise on small batches does not
	// inflate the ratio (10-org totals can measure near timer resolution).
	const minPerOrg = 100 * time.Nanosecond
	perOrg10 := times[10] / 10
	perOrg100 := times[100] / 100
	if perOrg10 < minPerOrg {
		t.Skipf("10-org per-org time %v below measurement floor %v", perOrg10, minPerOrg)
	}
	ratio := float64(perOrg100) / float64(perOrg10)
	// Cached map lookup should stay roughly O(1) per org; allow headroom for mutex/map growth.
	if ratio > 8.0 {
		t.Errorf("100-org per-org %v vs 10-org per-org %v (ratio %.2fx) suggests super-linear scaling",
			perOrg100, perOrg10, ratio)
	}

	// Batch totals should scale ~linearly (≈10x), not quadratically (100x).
	batchRatio := float64(times[100]) / float64(times[10])
	if batchRatio > 25.0 {
		t.Errorf("100-org batch %v vs 10-org batch %v (ratio %.2fx) suggests super-linear batch scaling",
			times[100], times[10], batchRatio)
	}
}
