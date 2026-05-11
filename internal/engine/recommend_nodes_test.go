package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultNodeRecConfig() NodeRecConfig {
	return NodeRecConfig{
		UnderutilThreshold:    0.30,
		OvercommitThreshold:   1.50,
		AllocatableFactor:     0.93,
		MinDataDays:           3,
		StrandedHighThreshold: 0.70,
		StrandedLowThreshold:  0.25,
		EMAAlpha:              0.3,
	}
}

func makeDigestRow(node string, day int, cpuP50, cpuP95, memP50, memP95, cpuReqs, memReqs int64, allocCPU, allocMem *int64) NodeDigestRow {
	return NodeDigestRow{
		BucketDate:        time.Date(2026, 5, day, 0, 0, 0, 0, time.UTC),
		Node:              node,
		CPUUsageP50MC:     cpuP50,
		CPUUsageP95MC:     cpuP95,
		MemUsageP50KiB:    memP50,
		MemUsageP95KiB:    memP95,
		MaxCPUAllocMC:     allocCPU,
		MaxMemAllocKiB:    allocMem,
		MaxCPURequestsMC:  cpuReqs,
		MaxMemRequestsKiB: memReqs,
		MaxPodCount:       10,
		SampleCount:       24,
	}
}

func ptr64(v int64) *int64 { return &v }

func TestRecommendNodes_MinDataDaysNotMet(t *testing.T) {
	cfg := defaultNodeRecConfig()
	digests := []NodeDigestRow{
		makeDigestRow("node-1", 1, 1000, 2000, 5000, 8000, 8000, 16000, ptr64(16000), ptr64(64000)),
		makeDigestRow("node-1", 2, 1000, 2000, 5000, 8000, 8000, 16000, ptr64(16000), ptr64(64000)),
	}

	results := RecommendNodes(digests, cfg)
	assert.Empty(t, results, "should not produce recs with < 3 days of data")
}

func TestRecommendNodes_Underutilized(t *testing.T) {
	cfg := defaultNodeRecConfig()
	allocCPU := ptr64(16000)  // 16 cores in millicores
	allocMem := ptr64(65536)  // 64 GiB in KiB

	digests := []NodeDigestRow{
		makeDigestRow("node-idle", 1, 500, 1000, 2000, 4000, 8000, 32000, allocCPU, allocMem),
		makeDigestRow("node-idle", 2, 600, 1200, 2500, 4500, 8000, 32000, allocCPU, allocMem),
		makeDigestRow("node-idle", 3, 550, 1100, 2200, 4200, 8000, 32000, allocCPU, allocMem),
		makeDigestRow("node-idle", 4, 500, 900, 2000, 3800, 8000, 32000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg)
	require.Len(t, results, 1)
	rec := results[0]

	assert.True(t, rec.IsUnderutilized, "node should be flagged as underutilized")
	assert.False(t, rec.IsOvercommitted)
	assert.Nil(t, rec.StrandedResource)
	assert.Contains(t, rec.NotificationCodes, NotifNodeUnderutilized)
}

func TestRecommendNodes_Overcommitted(t *testing.T) {
	cfg := defaultNodeRecConfig()
	allocCPU := ptr64(8000)
	allocMem := ptr64(32768)

	digests := []NodeDigestRow{
		makeDigestRow("node-hot", 1, 6000, 7500, 20000, 28000, 14000, 30000, allocCPU, allocMem),
		makeDigestRow("node-hot", 2, 6200, 7800, 21000, 29000, 14000, 30000, allocCPU, allocMem),
		makeDigestRow("node-hot", 3, 6100, 7600, 20500, 28500, 14000, 30000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg)
	require.Len(t, results, 1)
	rec := results[0]

	assert.True(t, rec.IsOvercommitted, "node should be flagged as overcommitted")
	assert.False(t, rec.IsUnderutilized)
	assert.Contains(t, rec.NotificationCodes, NotifNodeOvercommitted)
	assert.True(t, rec.CPUOvercommitRatio > 1.5)
}

func TestRecommendNodes_StrandedCPU(t *testing.T) {
	cfg := defaultNodeRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	// High memory utilization, low CPU → stranded CPU
	digests := []NodeDigestRow{
		makeDigestRow("node-mem", 1, 1000, 2000, 50000, 55000, 8000, 60000, allocCPU, allocMem),
		makeDigestRow("node-mem", 2, 1200, 2200, 51000, 56000, 8000, 60000, allocCPU, allocMem),
		makeDigestRow("node-mem", 3, 1100, 2100, 50500, 55500, 8000, 60000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg)
	require.Len(t, results, 1)
	rec := results[0]

	require.NotNil(t, rec.StrandedResource)
	assert.Equal(t, "cpu", *rec.StrandedResource)
	assert.Contains(t, rec.NotificationCodes, NotifStrandedResources)
}

func TestRecommendNodes_StrandedMemory(t *testing.T) {
	cfg := defaultNodeRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	// High CPU utilization, low memory → stranded memory
	digests := []NodeDigestRow{
		makeDigestRow("node-cpu", 1, 12000, 14000, 5000, 8000, 14000, 32000, allocCPU, allocMem),
		makeDigestRow("node-cpu", 2, 12500, 14500, 5500, 8500, 14000, 32000, allocCPU, allocMem),
		makeDigestRow("node-cpu", 3, 12200, 14200, 5200, 8200, 14000, 32000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg)
	require.Len(t, results, 1)
	rec := results[0]

	require.NotNil(t, rec.StrandedResource)
	assert.Equal(t, "memory", *rec.StrandedResource)
	assert.Contains(t, rec.NotificationCodes, NotifStrandedResources)
}

func TestRecommendNodes_NormalNode(t *testing.T) {
	cfg := defaultNodeRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	// Moderate utilization — no flags
	digests := []NodeDigestRow{
		makeDigestRow("node-ok", 1, 8000, 10000, 30000, 40000, 12000, 48000, allocCPU, allocMem),
		makeDigestRow("node-ok", 2, 8500, 10500, 32000, 42000, 12000, 48000, allocCPU, allocMem),
		makeDigestRow("node-ok", 3, 8200, 10200, 31000, 41000, 12000, 48000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg)
	require.Len(t, results, 1)
	rec := results[0]

	assert.False(t, rec.IsUnderutilized)
	assert.False(t, rec.IsOvercommitted)
	assert.Nil(t, rec.StrandedResource)
	assert.Empty(t, rec.NotificationCodes)
}

func TestRecommendNodes_MultipleNodes(t *testing.T) {
	cfg := defaultNodeRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	digests := []NodeDigestRow{
		// Node A: underutilized
		makeDigestRow("node-a", 1, 500, 1000, 2000, 4000, 8000, 32000, allocCPU, allocMem),
		makeDigestRow("node-a", 2, 600, 1200, 2500, 4500, 8000, 32000, allocCPU, allocMem),
		makeDigestRow("node-a", 3, 550, 1100, 2200, 4200, 8000, 32000, allocCPU, allocMem),
		// Node B: normal
		makeDigestRow("node-b", 1, 8000, 10000, 30000, 40000, 12000, 48000, allocCPU, allocMem),
		makeDigestRow("node-b", 2, 8500, 10500, 32000, 42000, 12000, 48000, allocCPU, allocMem),
		makeDigestRow("node-b", 3, 8200, 10200, 31000, 41000, 12000, 48000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg)
	require.Len(t, results, 2)

	recMap := map[string]NodeRec{}
	for _, r := range results {
		recMap[r.Node] = r
	}

	assert.True(t, recMap["node-a"].IsUnderutilized)
	assert.False(t, recMap["node-b"].IsUnderutilized)
}

func TestRecommendNodes_NoAllocatable_FallsBackToRequests(t *testing.T) {
	cfg := defaultNodeRecConfig()

	// No allocatable data (nil pointers), only requests available
	digests := []NodeDigestRow{
		makeDigestRow("node-nap", 1, 500, 1000, 2000, 4000, 8000, 32000, nil, nil),
		makeDigestRow("node-nap", 2, 600, 1200, 2500, 4500, 8000, 32000, nil, nil),
		makeDigestRow("node-nap", 3, 550, 1100, 2200, 4200, 8000, 32000, nil, nil),
	}

	results := RecommendNodes(digests, cfg)
	require.Len(t, results, 1)
	// With requests=8000 and factor=0.93, effective alloc = 8000/0.93 ≈ 8602
	// CPU p95 avg ≈ 1100/8602 ≈ 0.128 (below 0.30 threshold)
	assert.True(t, results[0].IsUnderutilized)
}

func TestRecommendNodes_StrandedThresholdsConfigurable(t *testing.T) {
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	// CPU p95 = 10000/16000 = 0.625, Mem p95 = 8000/65536 = 0.122
	// With defaults (high=0.70, low=0.25), no stranded detection
	// With relaxed thresholds (high=0.60, low=0.15), should detect stranded memory
	digests := []NodeDigestRow{
		makeDigestRow("node-x", 1, 9000, 10000, 7000, 8000, 12000, 32000, allocCPU, allocMem),
		makeDigestRow("node-x", 2, 9200, 10200, 7200, 8200, 12000, 32000, allocCPU, allocMem),
		makeDigestRow("node-x", 3, 9100, 10100, 7100, 8100, 12000, 32000, allocCPU, allocMem),
	}

	// Default thresholds: not stranded
	cfgDefault := defaultNodeRecConfig()
	results := RecommendNodes(digests, cfgDefault)
	require.Len(t, results, 1)
	assert.Nil(t, results[0].StrandedResource, "should not detect stranded with default thresholds")

	// Relaxed thresholds: now detects stranded memory
	cfgRelaxed := defaultNodeRecConfig()
	cfgRelaxed.StrandedHighThreshold = 0.60
	cfgRelaxed.StrandedLowThreshold = 0.15
	results = RecommendNodes(digests, cfgRelaxed)
	require.Len(t, results, 1)
	require.NotNil(t, results[0].StrandedResource, "should detect stranded with relaxed thresholds")
	assert.Equal(t, "memory", *results[0].StrandedResource)
}

func TestEmaSmooth(t *testing.T) {
	// Empty input returns empty
	assert.Equal(t, []float64(nil), emaSmooth(nil, 0.3))
	assert.Equal(t, []float64{}, emaSmooth([]float64{}, 0.3))

	// Single element: returned as-is
	result := emaSmooth([]float64{5.0}, 0.3)
	assert.Equal(t, []float64{5.0}, result)

	// Smoothing dampens spikes
	noisy := []float64{0.5, 0.5, 0.5, 2.0, 0.5, 0.5}
	smoothed := emaSmooth(noisy, 0.3)
	assert.Equal(t, 0.5, smoothed[0])
	// The spike at index 3 should be dampened
	assert.True(t, smoothed[3] < 2.0, "spike should be dampened")
	assert.True(t, smoothed[3] > 0.5, "spike should still raise the value")
	// After the spike, values should decay back toward 0.5
	assert.True(t, smoothed[5] < smoothed[3], "should decay after spike")
}

func TestEmaSmooth_PreservesMonotonicTrend(t *testing.T) {
	increasing := []float64{0.1, 0.2, 0.3, 0.4, 0.5}
	smoothed := emaSmooth(increasing, 0.3)
	// Smoothed values should still be monotonically increasing
	for i := 1; i < len(smoothed); i++ {
		assert.True(t, smoothed[i] > smoothed[i-1], "smoothed series should be monotonically increasing")
	}
}

func TestLinearRegressionSlope(t *testing.T) {
	// Perfect increasing line: y = 0.1 * x
	ys := []float64{0.0, 0.1, 0.2, 0.3, 0.4}
	slope := linearRegressionSlope(ys)
	assert.InDelta(t, 0.1, slope, 0.001)

	// Constant — slope should be 0
	constant := []float64{0.5, 0.5, 0.5, 0.5}
	slope = linearRegressionSlope(constant)
	assert.InDelta(t, 0.0, slope, 0.001)

	// Decreasing
	decreasing := []float64{1.0, 0.8, 0.6, 0.4}
	slope = linearRegressionSlope(decreasing)
	assert.True(t, slope < 0)
}

func TestTrendSlope_SpikesDampened(t *testing.T) {
	cfg := defaultNodeRecConfig()
	allocCPU := ptr64(10000)
	allocMem := ptr64(65536)

	// Steady node with a single-day spike at day 3
	digests := []NodeDigestRow{
		makeDigestRow("node-spike", 1, 5000, 6000, 30000, 40000, 8000, 48000, allocCPU, allocMem),
		makeDigestRow("node-spike", 2, 5100, 6100, 30000, 40000, 8000, 48000, allocCPU, allocMem),
		makeDigestRow("node-spike", 3, 9000, 9500, 30000, 40000, 8000, 48000, allocCPU, allocMem), // spike
		makeDigestRow("node-spike", 4, 5000, 6000, 30000, 40000, 8000, 48000, allocCPU, allocMem),
		makeDigestRow("node-spike", 5, 5050, 6050, 30000, 40000, 8000, 48000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg)
	require.Len(t, results, 1)
	// With EMA smoothing, the spike should be dampened and the trend should be
	// approximately flat (near zero) rather than strongly positive
	assert.InDelta(t, 0.0, float64(results[0].TrendSlope), 0.05,
		"EMA-smoothed trend should be near-zero for a node with a single spike")
}
