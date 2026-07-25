package node

import (
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/fixedpoint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var defaultThresholdSettings = DefaultThresholdSettings()

func defaultRecConfig() RecConfig {
	return RecConfig{
		UnderutilThresholdBP:         fixedpoint.FloatToBasisPoints(0.30),
		OvercommitThresholdBP:        fixedpoint.RatioToBasisPoints(1.50),
		AllocatableFactor:            0.93,
		StrandedImbalanceThresholdBP: fixedpoint.FloatToBasisPoints(0.6),
		EMAAlpha:                     0.3,
	}
}

func singleMediumTerm() []core.TermConfig {
	return []core.TermConfig{
		{Name: "medium", WindowDays: 30, MinDataDays: 3},
	}
}

func makeDigestRow(node string, day int, cpuP50, cpuP95, memP50, memP95, cpuReqs, memReqs int64, allocCPU, allocMem *int64) DigestRow {
	return DigestRow{
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

func makeDigestRowWithPods(node string, day int, cpuP50, cpuP95, memP50, memP95, cpuReqs, memReqs, maxPods, podCapacity int64, allocCPU, allocMem *int64) DigestRow {
	r := makeDigestRow(node, day, cpuP50, cpuP95, memP50, memP95, cpuReqs, memReqs, allocCPU, allocMem)
	r.MaxPodCount = maxPods
	r.PodCapacity = podCapacity
	return r
}

func recsByNodeEngine(recs []Rec) map[string]Rec {
	m := make(map[string]Rec, len(recs))
	for _, r := range recs {
		m[r.Node+"/"+r.Engine] = r
	}
	return m
}

func recsForNode(recs []Rec, node string) []Rec {
	var out []Rec
	for _, r := range recs {
		if r.Node == node {
			out = append(out, r)
		}
	}
	return out
}

func TestRecommendNodes_ConfidenceLevel(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)
	terms := []core.TermConfig{{Name: "medium", WindowDays: 30, MinDataDays: 3}}

	var partial []DigestRow
	for day := 1; day <= 4; day++ {
		partial = append(partial, makeDigestRow("node-partial", day, 500, 1000, 2000, 4000, 8000, 32000, allocCPU, allocMem))
	}
	partialResults := RecommendNodes(partial, cfg, defaultThresholdSettings, terms)
	require.Len(t, partialResults, 2)
	wantPartial := core.ComputeConfidence(4, 3, 30)
	assert.InDelta(t, wantPartial, partialResults[0].ConfidenceLevel, 0.001)
	assert.Equal(t, 4, partialResults[0].DataDays)
	assert.Contains(t, partialResults[0].NotificationCodes, core.NotifLowConfidence)

	var full []DigestRow
	for day := 1; day <= 30; day++ {
		full = append(full, makeDigestRow("node-full", day, 500, 1000, 2000, 4000, 8000, 32000, allocCPU, allocMem))
	}
	fullResults := RecommendNodes(full, cfg, defaultThresholdSettings, terms)
	require.Len(t, fullResults, 2)
	assert.InDelta(t, float32(1.0), fullResults[0].ConfidenceLevel, 0.001)
	assert.Equal(t, 30, fullResults[0].DataDays)
	assert.NotContains(t, fullResults[0].NotificationCodes, core.NotifLowConfidence)
}

func TestEvaluateNodeNotifications_SparseData(t *testing.T) {
	codes := evaluateNodeNotifications(nil, 1.0, 1)
	assert.Contains(t, codes, core.NotifSparseData)
}

func TestEvaluateNodeNotifications_SparseData_ExactThreshold(t *testing.T) {
	codes := evaluateNodeNotifications(nil, 1.0, defaultSparseDataThreshold)
	assert.Contains(t, codes, core.NotifSparseData, "data_days == threshold should fire")
}

func TestEvaluateNodeNotifications_SparseData_AboveThreshold(t *testing.T) {
	codes := evaluateNodeNotifications(nil, 1.0, defaultSparseDataThreshold+1)
	assert.NotContains(t, codes, core.NotifSparseData, "data_days above threshold should not fire")
}

func TestEvaluateNodeNotifications_SparseData_ZeroDays(t *testing.T) {
	codes := evaluateNodeNotifications(nil, 0.2, 0)
	assert.NotContains(t, codes, core.NotifSparseData, "zero data days should not fire SPARSE_DATA")
}

func TestEvaluateNodeNotifications_SparseData_OrthogonalToLowConfidence(t *testing.T) {
	codes := evaluateNodeNotifications(nil, 1.0, 1)
	assert.Contains(t, codes, core.NotifSparseData, "sparse data should fire even with high confidence")
	assert.NotContains(t, codes, core.NotifLowConfidence, "low confidence should NOT fire with confidence=1.0")
}

func TestRecommendNodes_SparseDataViaShortTerm(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	digests := []DigestRow{
		makeDigestRow("node-sparse", 1, 500, 1000, 2000, 4000, 8000, 32000, allocCPU, allocMem),
		makeDigestRow("node-sparse", 2, 600, 1200, 2500, 4500, 8000, 32000, allocCPU, allocMem),
	}

	terms := []core.TermConfig{{Name: "short", WindowDays: 7, MinDataDays: 1}}
	results := RecommendNodes(digests, cfg, defaultThresholdSettings, terms)
	require.NotEmpty(t, results)

	shortCost := recsByNodeEngine(results)["node-sparse/cost"]
	require.NotEmpty(t, shortCost.Node)
	assert.Equal(t, 2, shortCost.DataDays)
	assert.Contains(t, shortCost.NotificationCodes, core.NotifSparseData)
}

func TestRecommendNodes_MinDataDaysNotMet(t *testing.T) {
	cfg := defaultRecConfig()
	digests := []DigestRow{
		makeDigestRow("node-1", 1, 1000, 2000, 5000, 8000, 8000, 16000, ptr64(16000), ptr64(64000)),
		makeDigestRow("node-1", 2, 1000, 2000, 5000, 8000, 8000, 16000, ptr64(16000), ptr64(64000)),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	assert.Empty(t, results, "should not produce recs with < 3 days of data")
}

func TestRecommendNodes_Underutilized(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(16000) // 16 cores in millicores
	allocMem := ptr64(65536) // 64 GiB in KiB

	// CPU ~12-15%, mem ~9-15%: above idle threshold (10%) but below underutil threshold (30%)
	// PodCount=15 (via makeDigestRowWithPods): above idle max pods (10) to prevent idle classification
	digests := []DigestRow{
		makeDigestRowWithPods("node-underutil", 1, 1800, 2000, 5000, 8000, 8000, 32000, 15, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-underutil", 2, 2000, 2400, 5500, 9000, 8000, 32000, 15, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-underutil", 3, 1900, 2200, 5200, 8500, 8000, 32000, 15, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-underutil", 4, 1800, 2100, 5000, 8200, 8000, 32000, 15, 100, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	require.Len(t, results, 2)

	byEngine := recsByNodeEngine(results)
	costRec := byEngine["node-underutil/cost"]
	perfRec := byEngine["node-underutil/performance"]
	require.NotEmpty(t, costRec.Node)
	require.NotEmpty(t, perfRec.Node)

	assert.Equal(t, "underutilized", costRec.Category, "node should have underutilized category")
	assert.Equal(t, "underutilized", perfRec.Category)
	assert.Nil(t, costRec.StrandedResource)
	assert.Contains(t, costRec.NotificationCodes, core.NotifNodeUnderutilized)
	assert.Equal(t, costRec.Category, perfRec.Category)
	assert.Equal(t, costRec.StrandedResource, perfRec.StrandedResource)
}

func TestRecommendNodes_Overcommitted(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(8000)
	allocMem := ptr64(32768)

	digests := []DigestRow{
		makeDigestRow("node-hot", 1, 6000, 7500, 20000, 28000, 14000, 30000, allocCPU, allocMem),
		makeDigestRow("node-hot", 2, 6200, 7800, 21000, 29000, 14000, 30000, allocCPU, allocMem),
		makeDigestRow("node-hot", 3, 6100, 7600, 20500, 28500, 14000, 30000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	require.Len(t, results, 2)
	costRec := recsByNodeEngine(results)["node-hot/cost"]

	assert.Equal(t, "overcommitted", costRec.Category, "node should have overcommitted category")
	assert.Contains(t, costRec.NotificationCodes, core.NotifNodeOvercommitted)
	assert.True(t, costRec.CPUOvercommitRatio > 1.5)
}

func TestRecommendNodes_StrandedCPU(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	// High memory utilization, low CPU → stranded CPU
	digests := []DigestRow{
		makeDigestRow("node-mem", 1, 1000, 2000, 50000, 55000, 8000, 60000, allocCPU, allocMem),
		makeDigestRow("node-mem", 2, 1200, 2200, 51000, 56000, 8000, 60000, allocCPU, allocMem),
		makeDigestRow("node-mem", 3, 1100, 2100, 50500, 55500, 8000, 60000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	require.Len(t, results, 2)
	rec := recsByNodeEngine(results)["node-mem/cost"]

	require.NotNil(t, rec.StrandedResource)
	assert.Equal(t, "cpu", *rec.StrandedResource)
	assert.Contains(t, rec.NotificationCodes, core.NotifStrandedResources)
}

func TestClassifyNode_DecayWeightsRecentSpikeHigher(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)
	endDate := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)

	// Old low-util days and one recent high-util day; decay should pull average toward the spike.
	days := []DigestRow{
		makeDigestRow("node-decay", 1, 500, 1000, 2000, 4000, 4000, 16000, allocCPU, allocMem),
		makeDigestRow("node-decay", 2, 500, 1000, 2000, 4000, 4000, 16000, allocCPU, allocMem),
		makeDigestRow("node-decay", 3, 500, 1000, 2000, 4000, 4000, 16000, allocCPU, allocMem),
		makeDigestRow("node-decay", 10, 12000, 14000, 50000, 55000, 14000, 60000, allocCPU, allocMem),
	}

	withDecay := classifyNode("node-decay", days, cfg, defaultThresholdSettings, 168, endDate)
	equalWeight := classifyNode("node-decay", days, cfg, defaultThresholdSettings, 0, endDate)

	assert.Greater(t, withDecay.CPUUtilP95, equalWeight.CPUUtilP95,
		"medium-term decay should weight the recent CPU spike more than equal daily averages")
}

func TestRecommendNodes_StrandedMemory(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	// High CPU utilization, low memory → stranded memory
	digests := []DigestRow{
		makeDigestRow("node-cpu", 1, 12000, 14000, 5000, 8000, 14000, 32000, allocCPU, allocMem),
		makeDigestRow("node-cpu", 2, 12500, 14500, 5500, 8500, 14000, 32000, allocCPU, allocMem),
		makeDigestRow("node-cpu", 3, 12200, 14200, 5200, 8200, 14000, 32000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	require.Len(t, results, 2)
	rec := recsByNodeEngine(results)["node-cpu/cost"]

	require.NotNil(t, rec.StrandedResource)
	assert.Equal(t, "memory", *rec.StrandedResource)
	assert.Contains(t, rec.NotificationCodes, core.NotifStrandedResources)
}

func TestRecommendNodes_NormalNode(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	// Moderate utilization — no flags; use full window so low-confidence notification is not emitted
	var digests []DigestRow
	for day := 1; day <= 30; day++ {
		digests = append(digests, makeDigestRow("node-ok", day, 8000, 10000, 30000, 40000, 12000, 48000, allocCPU, allocMem))
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	require.Len(t, results, 2)
	rec := recsByNodeEngine(results)["node-ok/cost"]
	assert.Equal(t, "optimized", rec.Category)
	assert.Nil(t, rec.StrandedResource)
	assert.Empty(t, rec.NotificationCodes)
}

func TestRecommendNodes_MultipleNodes(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	digests := []DigestRow{
		// Node A: underutilized (above idle thresholds but below underutil 30%)
		makeDigestRowWithPods("node-a", 1, 1800, 2000, 5000, 8000, 8000, 32000, 15, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-a", 2, 2000, 2400, 5500, 9000, 8000, 32000, 15, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-a", 3, 1900, 2200, 5200, 8500, 8000, 32000, 15, 100, allocCPU, allocMem),
		// Node B: normal
		makeDigestRowWithPods("node-b", 1, 8000, 10000, 30000, 40000, 12000, 48000, 15, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-b", 2, 8500, 10500, 32000, 42000, 12000, 48000, 15, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-b", 3, 8200, 10200, 31000, 41000, 12000, 48000, 15, 100, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	require.Len(t, results, 4)

	recMap := recsByNodeEngine(results)

	assert.Equal(t, "underutilized", recMap["node-a/cost"].Category)
	assert.NotEqual(t, "underutilized", recMap["node-b/cost"].Category)
}

func TestRecommendNodes_NoAllocatable_FallsBackToRequests(t *testing.T) {
	cfg := defaultRecConfig()

	// No allocatable data (nil pointers), only requests available.
	// Uses 15 pods to avoid idle classification (idle threshold is 10 pods).
	digests := []DigestRow{
		makeDigestRowWithPods("node-nap", 1, 500, 1000, 2000, 4000, 8000, 32000, 15, 100, nil, nil),
		makeDigestRowWithPods("node-nap", 2, 600, 1200, 2500, 4500, 8000, 32000, 15, 100, nil, nil),
		makeDigestRowWithPods("node-nap", 3, 550, 1100, 2200, 4200, 8000, 32000, 15, 100, nil, nil),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	require.Len(t, results, 2)
	assert.Equal(t, "underutilized", recsByNodeEngine(results)["node-nap/cost"].Category)
}

func TestRecommendNodes_StrandedImbalanceConfigurable(t *testing.T) {
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	// CPU p95 = 10000/16000 = 0.625, Mem p95 = 20000/65536 = 0.305
	// imbalance = |0.625 - 0.305| / 0.625 = 0.512 — below 0.6 default
	digests := []DigestRow{
		makeDigestRow("node-x", 1, 9000, 10000, 18000, 20000, 12000, 32000, allocCPU, allocMem),
		makeDigestRow("node-x", 2, 9200, 10200, 18500, 20500, 12000, 32000, allocCPU, allocMem),
		makeDigestRow("node-x", 3, 9100, 10100, 18200, 20200, 12000, 32000, allocCPU, allocMem),
	}

	// Default threshold (0.6): not stranded (imbalance ~0.51)
	cfgDefault := defaultRecConfig()
	results := RecommendNodes(digests, cfgDefault, defaultThresholdSettings, singleMediumTerm())
	require.Len(t, results, 2)
	assert.Nil(t, recsByNodeEngine(results)["node-x/cost"].StrandedResource, "should not detect stranded with default 0.6 threshold")

	// Lowered threshold (0.4): now detects stranded memory (cpu > mem)
	cfgLowered := defaultRecConfig()
	cfgLowered.StrandedImbalanceThresholdBP = fixedpoint.FloatToBasisPoints(0.4)
	results = RecommendNodes(digests, cfgLowered, defaultThresholdSettings, singleMediumTerm())
	require.Len(t, results, 2)
	stranded := recsByNodeEngine(results)["node-x/cost"].StrandedResource
	require.NotNil(t, stranded, "should detect stranded with lowered threshold")
	assert.Equal(t, "memory", *stranded)
}

func TestRecommendNodes_StrandedTransientSpikeDampened(t *testing.T) {
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	// Days 1-4: balanced (CPU ~0.50, MEM ~0.46). Day 3: transient spike (CPU ~0.88, MEM ~0.12)
	digests := []DigestRow{
		makeDigestRow("node-t", 1, 7500, 8000, 28000, 30000, 12000, 48000, allocCPU, allocMem),
		makeDigestRow("node-t", 2, 7600, 8100, 28500, 30500, 12000, 48000, allocCPU, allocMem),
		makeDigestRow("node-t", 3, 13000, 14000, 7000, 8000, 12000, 48000, allocCPU, allocMem), // spike
		makeDigestRow("node-t", 4, 7700, 8200, 29000, 31000, 12000, 48000, allocCPU, allocMem),
		makeDigestRow("node-t", 5, 7500, 8000, 28000, 30000, 12000, 48000, allocCPU, allocMem),
	}

	cfg := defaultRecConfig()
	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	require.Len(t, results, 2)
	assert.Nil(t, recsByNodeEngine(results)["node-t/cost"].StrandedResource,
		"single-day spike should be dampened by EMA and not trigger stranded detection")
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
	slope := LinearRegressionSlope(ys)
	assert.InDelta(t, 0.1, slope, 0.001)

	// Constant — slope should be 0
	constant := []float64{0.5, 0.5, 0.5, 0.5}
	slope = LinearRegressionSlope(constant)
	assert.InDelta(t, 0.0, slope, 0.001)

	// Decreasing
	decreasing := []float64{1.0, 0.8, 0.6, 0.4}
	slope = LinearRegressionSlope(decreasing)
	assert.True(t, slope < 0)
}

func TestTrendSlope_SpikesDampened(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(10000)
	allocMem := ptr64(65536)

	// Steady node with a single-day spike at day 3
	digests := []DigestRow{
		makeDigestRow("node-spike", 1, 5000, 6000, 30000, 40000, 8000, 48000, allocCPU, allocMem),
		makeDigestRow("node-spike", 2, 5100, 6100, 30000, 40000, 8000, 48000, allocCPU, allocMem),
		makeDigestRow("node-spike", 3, 9000, 9500, 30000, 40000, 8000, 48000, allocCPU, allocMem), // spike
		makeDigestRow("node-spike", 4, 5000, 6000, 30000, 40000, 8000, 48000, allocCPU, allocMem),
		makeDigestRow("node-spike", 5, 5050, 6050, 30000, 40000, 8000, 48000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	require.Len(t, results, 2)
	assert.InDelta(t, 0.0, float64(recsByNodeEngine(results)["node-spike/cost"].TrendSlope), 0.05,
		"EMA-smoothed trend should be near-zero for a node with a single spike")
}

func TestRecommendNodes_ShortTermWithFutureEnd(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	digests := []DigestRow{
		makeDigestRow("node-recent", 1, 500, 1000, 2000, 4000, 8000, 32000, allocCPU, allocMem),
		makeDigestRow("node-recent", 2, 600, 1200, 2500, 4500, 8000, 32000, allocCPU, allocMem),
		makeDigestRow("node-recent", 3, 550, 1100, 2200, 4200, 8000, 32000, allocCPU, allocMem),
	}

	terms := []core.TermConfig{
		{Name: "short", WindowDays: 1, MinDataDays: 1},
		{Name: "medium", WindowDays: 7, MinDataDays: 3},
		{Name: "long", WindowDays: 15, MinDataDays: 7},
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, terms)

	termMap := map[string]Rec{}
	for _, r := range results {
		if r.Engine == "cost" {
			termMap[r.Term] = r
		}
	}

	require.Contains(t, termMap, "short", "short-term should be produced with 1 day of data")
	require.Contains(t, termMap, "medium", "medium-term should be produced with 3 days of data")
	assert.NotContains(t, termMap, "long", "long-term requires 7 days but only 3 available")

	assert.Equal(t, "short", termMap["short"].Term)
	assert.Equal(t, "medium", termMap["medium"].Term)
}

func TestFilterNodeByWindow(t *testing.T) {
	rows := []DigestRow{
		{BucketDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Node: "n1"},
		{BucketDate: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), Node: "n1"},
		{BucketDate: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), Node: "n1"},
		{BucketDate: time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC), Node: "n1"},
		{BucketDate: time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), Node: "n1"},
	}

	endDate := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)

	result := filterNodeByWindow(rows, endDate, 1)
	require.Len(t, result, 1)
	assert.Equal(t, 5, result[0].BucketDate.Day())

	result = filterNodeByWindow(rows, endDate, 3)
	require.Len(t, result, 3)

	result = filterNodeByWindow(rows, endDate, 30)
	require.Len(t, result, 5)
}

func TestRecommendNodes_DualEnginesPerNodeTerm(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	digests := []DigestRow{
		makeDigestRow("node-dual", 1, 8000, 10000, 30000, 40000, 12000, 48000, allocCPU, allocMem),
		makeDigestRow("node-dual", 2, 8500, 10500, 32000, 42000, 12000, 48000, allocCPU, allocMem),
		makeDigestRow("node-dual", 3, 8200, 10200, 31000, 41000, 12000, 48000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	require.Len(t, results, 2)

	engines := map[string]bool{}
	for _, r := range results {
		engines[r.Engine] = true
	}
	assert.True(t, engines["cost"])
	assert.True(t, engines["performance"])
}

func TestRecommendNodes_CostEngineSmallerCapacityThanPerformance(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	digests := []DigestRow{
		makeDigestRow("node-size", 1, 8000, 10000, 30000, 40000, 12000, 48000, allocCPU, allocMem),
		makeDigestRow("node-size", 2, 8500, 10500, 32000, 42000, 12000, 48000, allocCPU, allocMem),
		makeDigestRow("node-size", 3, 8200, 10200, 31000, 41000, 12000, 48000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	byEngine := recsByNodeEngine(results)
	costRec := byEngine["node-size/cost"]
	perfRec := byEngine["node-size/performance"]

	assert.LessOrEqual(t, costRec.RecommendedCPUMC, perfRec.RecommendedCPUMC)
	assert.LessOrEqual(t, costRec.RecommendedMemKiB, perfRec.RecommendedMemKiB)
}

func TestClassifyNode_PodSchedulingHeadroom(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(8000)
	allocMem := ptr64(32768)
	days := []DigestRow{
		makeDigestRowWithPods("node-pods", 1, 1500, 2000, 4000, 6000, 4000, 24000, 95, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-pods", 2, 1600, 2100, 4200, 6200, 4000, 24000, 95, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-pods", 3, 1550, 2050, 4100, 6100, 4000, 24000, 95, 100, allocCPU, allocMem),
	}
	endDate := days[len(days)-1].BucketDate

	class := classifyNode("node-pods", days, cfg, defaultThresholdSettings, 0, endDate)
	assert.InDelta(t, 0.05, float64(class.PodSchedulingHeadroom), 0.001)
	assert.Contains(t, class.NotificationCodes, core.NotifNodePodSchedulingLimit)

	unknown := classifyNode("node-unknown", []DigestRow{
		makeDigestRow("node-unknown", 1, 1500, 2000, 4000, 6000, 4000, 24000, allocCPU, allocMem),
	}, cfg, defaultThresholdSettings, 0, endDate)
	assert.Equal(t, float32(-1), unknown.PodSchedulingHeadroom)
}

func TestRecommendNodes_PodHeadroomUsesCustomSettings(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(8000)
	allocMem := ptr64(32768)
	days := []DigestRow{
		makeDigestRowWithPods("node-pods", 1, 1500, 2000, 4000, 6000, 4000, 24000, 95, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-pods", 2, 1600, 2100, 4200, 6200, 4000, 24000, 95, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-pods", 3, 1550, 2050, 4100, 6100, 4000, 24000, 95, 100, allocCPU, allocMem),
	}
	endDate := days[len(days)-1].BucketDate

	strictNotify := defaultThresholdSettings
	strictNotify.PodHeadroomNotificationThreshold = 0.03
	classStrict := classifyNode("node-pods", days, cfg, strictNotify, 0, endDate)
	assert.NotContains(t, classStrict.NotificationCodes, core.NotifNodePodSchedulingLimit)

	looseGate := defaultThresholdSettings
	looseGate.PodHeadroomConsolidationGate = 0.10
	digests := []DigestRow{
		makeDigestRowWithPods("node-saturated", 1, 1500, 2000, 4000, 6000, 4000, 24000, 88, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-saturated", 2, 1600, 2100, 4200, 6200, 4000, 24000, 88, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-saturated", 3, 1550, 2050, 4100, 6100, 4000, 24000, 88, 100, allocCPU, allocMem),
	}
	results := RecommendNodes(digests, cfg, looseGate, singleMediumTerm())
	costRec := recsByNodeEngine(results)["node-saturated/cost"]
	require.Equal(t, "underutilized", costRec.Category)
	assert.Equal(t, 1, costRec.NodeCountReduction, "looser consolidation gate should allow consolidation at 12% headroom")
}

func TestRecommendNodes_SuppressesConsolidationWhenPodSaturated(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(8000)
	allocMem := ptr64(32768)

	digests := []DigestRow{
		makeDigestRowWithPods("node-saturated", 1, 1500, 2000, 4000, 6000, 4000, 24000, 88, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-saturated", 2, 1600, 2100, 4200, 6200, 4000, 24000, 88, 100, allocCPU, allocMem),
		makeDigestRowWithPods("node-saturated", 3, 1550, 2050, 4100, 6100, 4000, 24000, 88, 100, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	costRec := recsByNodeEngine(results)["node-saturated/cost"]
	require.Equal(t, "underutilized", costRec.Category)
	assert.Equal(t, 0, costRec.NodeCountReduction, "pod-saturated node should not consolidate")
}

func TestRecommendNodes_CostEngineMoreAggressiveConsolidation(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(8000)
	allocMem := ptr64(32768)

	// Underutilized 8-core node: cost engine consolidates; performance keeps the node.
	digests := []DigestRow{
		makeDigestRow("node-consolidate", 1, 1500, 2000, 4000, 6000, 4000, 24000, allocCPU, allocMem),
		makeDigestRow("node-consolidate", 2, 1600, 2100, 4200, 6200, 4000, 24000, allocCPU, allocMem),
		makeDigestRow("node-consolidate", 3, 1550, 2050, 4100, 6100, 4000, 24000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	byEngine := recsByNodeEngine(results)
	costRec := byEngine["node-consolidate/cost"]
	perfRec := byEngine["node-consolidate/performance"]

	require.Equal(t, "underutilized", costRec.Category)
	assert.Equal(t, 1, costRec.NodeCountReduction, "cost engine should recommend consolidation when underutilized")
	assert.Equal(t, 0, perfRec.NodeCountReduction, "performance engine should not consolidate without extreme waste")
}

func TestRecommendNodes_EngineSavingsDiffer(t *testing.T) {
	cfg := defaultRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	digests := []DigestRow{
		makeDigestRow("node-savings", 1, 500, 1000, 2000, 4000, 8000, 32000, allocCPU, allocMem),
		makeDigestRow("node-savings", 2, 600, 1200, 2500, 4500, 8000, 32000, allocCPU, allocMem),
		makeDigestRow("node-savings", 3, 550, 1100, 2200, 4200, 8000, 32000, allocCPU, allocMem),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	byEngine := recsByNodeEngine(results)
	costRec := byEngine["node-savings/cost"]
	perfRec := byEngine["node-savings/performance"]

	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour":  {Infrastructure: 0, Supplementary: 0.01},
			"memory_gb_usage_per_hour": {Infrastructure: 0, Supplementary: 0.02},
			"node_cost_per_month":      {Infrastructure: 1000, Supplementary: 0},
		},
	}
	recs := []Rec{costRec, perfRec}
	ApplyNodeSavings(recs, cd)

	assert.Greater(t, recs[0].EstimatedMonthlySavingsCents, recs[1].EstimatedMonthlySavingsCents,
		"cost engine should show higher savings than performance for underutilized node")
}

func TestResolveAllocatable_PrefersStored(t *testing.T) {
	stored := int64(3500)
	assert.Equal(t, int64(3500), ResolveAllocatable(&stored, 8000, 0.93))
	assert.Equal(t, int64(3500), ResolveAllocatableMem(&stored, 32000, 0.93))
}

func TestResolveAllocatable_FallsBackToRequests(t *testing.T) {
	factor := 0.93
	assert.Equal(t, int64(8602), ResolveAllocatable(nil, 8000, factor))
	assert.Equal(t, int64(34408), ResolveAllocatableMem(nil, 32000, factor))
}

func TestNodeCapacityFleetKey_SimilarCapacitySameKey(t *testing.T) {
	// Within one decimal core/GiB bucket (8.0 cores, 32.0 GiB).
	classA := nodeClassification{CurrentCPUMC: 8000, CurrentMemKiB: 32 * 1024 * 1024}
	classB := nodeClassification{CurrentCPUMC: 8040, CurrentMemKiB: 32*1024*1024 + 50*1024}
	assert.Equal(t, nodeCapacityFleetKey(classA), nodeCapacityFleetKey(classB))
}

func TestNodeCapacityFleetKey_DifferentCapacityDifferentKey(t *testing.T) {
	classSmall := nodeClassification{CurrentCPUMC: 4000, CurrentMemKiB: 16 * 1024 * 1024}
	classLarge := nodeClassification{CurrentCPUMC: 16000, CurrentMemKiB: 64 * 1024 * 1024}
	assert.NotEqual(t, nodeCapacityFleetKey(classSmall), nodeCapacityFleetKey(classLarge))
}

func TestNodeCapacityFleetKey_EmptyWhenCapacityUnknown(t *testing.T) {
	assert.Empty(t, nodeCapacityFleetKey(nodeClassification{}))
	assert.Empty(t, nodeCapacityFleetKey(nodeClassification{CurrentCPUMC: 8000}))
}

func TestSuggestFleetInstanceType_CPUStrandedPicksLowerRatio(t *testing.T) {
	// m5.xlarge ~ 4:16 GiB = 0.25 mc/kib; c5.xlarge ~ 4:8 GiB = 0.5 — wait, c5 has less memory per CPU
	// Use explicit ratios: node is memory-heavy shape (low CPU:mem ratio target)
	fleet := map[string]float64{
		"m5.xlarge": 0.40,
		"c5.xlarge": 0.20,
	}
	suggested, reason := suggestFleetInstanceType("cpu", "m5.xlarge", 16000, 65536, fleet)
	assert.Equal(t, "c5.xlarge", suggested)
	assert.Contains(t, reason, "CPU-stranded")
}

func TestSuggestFleetInstanceType_MemoryStrandedPicksHigherRatio(t *testing.T) {
	fleet := map[string]float64{
		"r5.xlarge": 0.25,
		"c5.xlarge": 0.50,
	}
	suggested, reason := suggestFleetInstanceType("memory", "r5.xlarge", 16000, 65536, fleet)
	assert.Equal(t, "c5.xlarge", suggested)
	assert.Contains(t, reason, "Memory-stranded")
}

func TestSuggestFleetInstanceType_CPUStrandedSkipsHigherRatio(t *testing.T) {
	fleet := map[string]float64{
		"m5.xlarge": 0.24,
		"c5.xlarge": 0.48,
	}
	suggested, _ := suggestFleetInstanceType("cpu", "m5.xlarge", 16000, 65536, fleet)
	assert.Empty(t, suggested, "c5 has higher CPU:memory ratio, not a fit for CPU-stranded")
}

func TestSuggestFleetInstanceType_NoAlternativeEmpty(t *testing.T) {
	fleet := map[string]float64{
		"m5.xlarge": 0.40,
	}
	suggested, reason := suggestFleetInstanceType("cpu", "m5.xlarge", 16000, 65536, fleet)
	assert.Empty(t, suggested)
	assert.Empty(t, reason)
}

func TestRecommendNodes_StrandedCPU_SuggestsFleetInstanceType(t *testing.T) {
	cfg := defaultRecConfig()
	// m5: 16 CPU / 64 GiB; r5: 16 CPU / 128 GiB → lower CPU:memory ratio (more memory per CPU)
	allocM5CPU := ptr64(16000)
	allocM5Mem := ptr64(65536)
	allocR5CPU := ptr64(16000)
	allocR5Mem := ptr64(131072)

	digests := []DigestRow{
		makeDigestRowWithInstance("node-mem", 1, 1000, 2000, 50000, 55000, 8000, 60000, allocM5CPU, allocM5Mem, "m5.xlarge"),
		makeDigestRowWithInstance("node-mem", 2, 1200, 2200, 51000, 56000, 8000, 60000, allocM5CPU, allocM5Mem, "m5.xlarge"),
		makeDigestRowWithInstance("node-mem", 3, 1100, 2100, 50500, 55500, 8000, 60000, allocM5CPU, allocM5Mem, "m5.xlarge"),
		makeDigestRowWithInstance("node-r5", 1, 500, 800, 2000, 3000, 4000, 8000, allocR5CPU, allocR5Mem, "r5.xlarge"),
		makeDigestRowWithInstance("node-r5", 2, 520, 820, 2100, 3100, 4000, 8000, allocR5CPU, allocR5Mem, "r5.xlarge"),
		makeDigestRowWithInstance("node-r5", 3, 510, 810, 2050, 3050, 4000, 8000, allocR5CPU, allocR5Mem, "r5.xlarge"),
	}

	results := RecommendNodes(digests, cfg, defaultThresholdSettings, singleMediumTerm())
	rec := recsByNodeEngine(results)["node-mem/cost"]
	require.NotNil(t, rec.StrandedResource)
	assert.Equal(t, "cpu", *rec.StrandedResource)
	assert.Equal(t, "r5.xlarge", rec.SuggestedInstanceType)
	assert.NotEmpty(t, rec.InstanceTypeReason)
}

func makeDigestRowWithInstance(
	node string, day int,
	cpuP50, cpuP95, memP50, memP95, cpuReqs, memReqs int64,
	allocCPU, allocMem *int64, instanceType string,
) DigestRow {
	r := makeDigestRow(node, day, cpuP50, cpuP95, memP50, memP95, cpuReqs, memReqs, allocCPU, allocMem)
	r.InstanceType = instanceType
	return r
}
