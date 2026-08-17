package ingestion

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAggregateNodeDigests_EmptyRows(t *testing.T) {
	result := AggregateNodeDigests(nil)
	assert.Empty(t, result)
}

func TestAggregateNodeDigests_SkipsRowsWithoutNode(t *testing.T) {
	rows := []MetricRow{
		{IntervalStart: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC), Node: "", CPUUsageMC: 100, MemUsageKiB: 500},
	}
	result := AggregateNodeDigests(rows)
	assert.Empty(t, result, "rows without node should be skipped")
}

func TestAggregateNodeDigests_GroupsByNodeAndDay(t *testing.T) {
	day1_10am := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	day1_11am := time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC)
	day2_10am := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	rows := []MetricRow{
		{IntervalStart: day1_10am, Node: "node-a", CPUUsageMC: 100, MemUsageKiB: 500, CPURequestMC: 200, MemRequestKiB: 1000},
		{IntervalStart: day1_11am, Node: "node-a", CPUUsageMC: 150, MemUsageKiB: 600, CPURequestMC: 200, MemRequestKiB: 1000},
		{IntervalStart: day1_10am, Node: "node-b", CPUUsageMC: 300, MemUsageKiB: 2000, CPURequestMC: 500, MemRequestKiB: 4000},
		{IntervalStart: day2_10am, Node: "node-a", CPUUsageMC: 120, MemUsageKiB: 550, CPURequestMC: 200, MemRequestKiB: 1000},
	}

	result := AggregateNodeDigests(rows)
	// 3 unique (node, day) combinations: (node-a, day1), (node-b, day1), (node-a, day2)
	require.Len(t, result, 3)
}

func TestAggregateNodeDigests_DistinctPodsPerHour(t *testing.T) {
	interval := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	// Two containers in the same pod at the same interval count as one pod.
	rows := []MetricRow{
		{IntervalStart: interval, Node: "node-a", Pod: "app-1", CPUUsageMC: 100, MemUsageKiB: 500},
		{IntervalStart: interval, Node: "node-a", Pod: "app-1", CPUUsageMC: 50, MemUsageKiB: 200},
		{IntervalStart: interval, Node: "node-a", Pod: "app-2", CPUUsageMC: 80, MemUsageKiB: 400},
	}

	result := AggregateNodeDigests(rows)
	require.Len(t, result, 1)

	for _, acc := range result {
		_, _, _, _, _, _, _, _, maxPods, _ := acc.Finalize()
		assert.Equal(t, int64(2), maxPods, "max_pod_count should count distinct pods, not containers")
	}
}

func TestAggregateNodeDigests_AccumulatesPerInterval(t *testing.T) {
	interval := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	// Two containers on the same pod at the same interval
	rows := []MetricRow{
		{IntervalStart: interval, Node: "node-a", Pod: "pod-a", CPUUsageMC: 100, MemUsageKiB: 500, CPURequestMC: 200, MemRequestKiB: 1000},
		{IntervalStart: interval, Node: "node-a", Pod: "pod-a", CPUUsageMC: 150, MemUsageKiB: 600, CPURequestMC: 300, MemRequestKiB: 2000},
	}

	result := AggregateNodeDigests(rows)
	require.Len(t, result, 1)

	for _, acc := range result {
		cpuP50, cpuP95, cpuMax, memP50, memP95, memMax, maxCPUReq, maxMemReq, maxPods, sampleCount := acc.Finalize()

		// Both containers on same interval → single sample with sum = 250 CPU, 1100 mem
		assert.Equal(t, int64(250), cpuP50)
		assert.Equal(t, int64(250), cpuP95) // only 1 sample, so p50 == p95
		assert.Equal(t, int64(250), cpuMax)
		assert.Equal(t, int64(1100), memP50)
		assert.Equal(t, int64(1100), memP95)
		assert.Equal(t, int64(1100), memMax)
		assert.Equal(t, int64(500), maxCPUReq) // 200 + 300
		assert.Equal(t, int64(3000), maxMemReq)
		assert.Equal(t, int64(1), maxPods, "single pod with two containers")
		assert.Equal(t, int64(1), sampleCount) // 1 unique interval
	}
}

func TestAggregateNodeDigests_PercentilesComputed(t *testing.T) {
	// 10 intervals on the same day, same node — ascending CPU usage
	rows := make([]MetricRow, 10)
	for i := 0; i < 10; i++ {
		rows[i] = MetricRow{
			IntervalStart: time.Date(2026, 5, 1, i, 0, 0, 0, time.UTC),
			Node:          "node-x",
			CPUUsageMC:    int64((i + 1) * 100),  // 100, 200, ..., 1000
			MemUsageKiB:   int64((i + 1) * 1000), // 1000, 2000, ..., 10000
			CPURequestMC:  500,
			MemRequestKiB: 5000,
		}
	}

	result := AggregateNodeDigests(rows)
	require.Len(t, result, 1)

	for _, acc := range result {
		cpuP50, cpuP95, cpuMax, memP50, memP95, memMax, _, _, _, sampleCount := acc.Finalize()
		assert.Equal(t, int64(10), sampleCount)
		// Sorted: [100,200,300,400,500,600,700,800,900,1000]
		// P50 index: int(0.50 * 9) = 4 → 500
		assert.Equal(t, int64(500), cpuP50)
		// P95 index: int(0.95 * 9) = 8 → 900
		assert.Equal(t, int64(900), cpuP95)
		assert.Equal(t, int64(1000), cpuMax)
		assert.Equal(t, int64(5000), memP50)
		assert.Equal(t, int64(9000), memP95)
		assert.Equal(t, int64(10000), memMax)
	}
}

func TestAggregateNodeDigests_InstanceTypePreserved(t *testing.T) {
	interval := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	rows := []MetricRow{
		{IntervalStart: interval, Node: "node-a", InstanceType: "m5.xlarge", CPUUsageMC: 100, MemUsageKiB: 500},
		{IntervalStart: interval, Node: "node-a", InstanceType: "m5.xlarge", CPUUsageMC: 50, MemUsageKiB: 200},
		{IntervalStart: interval, Node: "node-b", InstanceType: "", CPUUsageMC: 80, MemUsageKiB: 400},
	}

	result := AggregateNodeDigests(rows)
	require.Len(t, result, 2)

	keyA := NodeDayKey{Node: "node-a", BucketDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	keyB := NodeDayKey{Node: "node-b", BucketDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	assert.Equal(t, "m5.xlarge", result[keyA].InstanceType)
	assert.Equal(t, "", result[keyB].InstanceType)
}

func TestAggregateNodeDigests_CapacityTracked(t *testing.T) {
	interval := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	rows := []MetricRow{
		{IntervalStart: interval, Node: "node-cap", CPUUsageMC: 100, MemUsageKiB: 500, NodeCapacityCPUMC: 8000, NodeCapacityMemKiB: 32768},
	}

	result := AggregateNodeDigests(rows)
	require.Len(t, result, 1)

	for _, acc := range result {
		assert.Equal(t, int64(8000), acc.MaxCPUCapacityMC)
		assert.Equal(t, int64(32768), acc.MaxMemCapacityKiB)
	}
}

func TestNodeDigestAllocatable_PrefersObserved(t *testing.T) {
	factor := 0.93
	v := nodeDigestAllocatable(3500, 4000, factor)
	require.NotNil(t, v)
	assert.Equal(t, int64(3500), *v)
}

func TestNodeDigestAllocatable_FallsBackToCapacityFactor(t *testing.T) {
	factor := 0.93
	v := nodeDigestAllocatable(0, 4000, factor)
	require.NotNil(t, v)
	assert.Equal(t, int64(3720), *v)
}

func TestNodeDigestAllocatable_NoData(t *testing.T) {
	assert.Nil(t, nodeDigestAllocatable(0, 0, 0.93))
}

func TestAggregateNodeDigests_AllocatableTracked(t *testing.T) {
	interval := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	rows := []MetricRow{
		{
			IntervalStart:         interval,
			Node:                  "node-a",
			NodeAllocatableCPUMC:  3500,
			NodeAllocatableMemKiB: 15000000,
		},
	}

	result := AggregateNodeDigests(rows)
	require.Len(t, result, 1)

	for _, acc := range result {
		assert.Equal(t, int64(3500), acc.MaxCPUAllocatableMC)
		assert.Equal(t, int64(15000000), acc.MaxMemAllocatableKiB)
	}
}

func TestAggregateNodeDigests_PodCapacityAndMachineSet(t *testing.T) {
	interval := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	rows := []MetricRow{
		{
			IntervalStart:   interval,
			Node:            "node-ms",
			NodePodCapacity: 250,
			MachineSetName:  "worker-us-east-1",
			CPUUsageMC:      100,
			MemUsageKiB:     500,
		},
	}
	result := AggregateNodeDigests(rows)
	require.Len(t, result, 1)
	for _, acc := range result {
		assert.Equal(t, int64(250), acc.MaxPodCapacity)
		assert.Equal(t, "worker-us-east-1", acc.MachineSetName)
	}
}

func TestPercentileInt64(t *testing.T) {
	assert.Equal(t, int64(0), percentileInt64(nil, 0.5))
	assert.Equal(t, int64(0), percentileInt64([]int64{}, 0.5))
	assert.Equal(t, int64(42), percentileInt64([]int64{42}, 0.5))

	sorted := []int64{10, 20, 30, 40, 50}
	assert.Equal(t, int64(30), percentileInt64(sorted, 0.50))
	assert.Equal(t, int64(40), percentileInt64(sorted, 0.75))
	assert.Equal(t, int64(40), percentileInt64(sorted, 0.95))
}

func TestFinalize_MaxIsLastSortedElement(t *testing.T) {
	// 3 intervals with known CPU/memory values — max should be the highest hourly aggregate.
	rows := []MetricRow{
		{IntervalStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Node: "node-max", CPUUsageMC: 100, MemUsageKiB: 500},
		{IntervalStart: time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC), Node: "node-max", CPUUsageMC: 300, MemUsageKiB: 1500},
		{IntervalStart: time.Date(2026, 5, 1, 2, 0, 0, 0, time.UTC), Node: "node-max", CPUUsageMC: 200, MemUsageKiB: 1000},
	}

	result := AggregateNodeDigests(rows)
	require.Len(t, result, 1)

	for _, acc := range result {
		_, _, cpuMax, _, _, memMax, _, _, _, _ := acc.Finalize()
		assert.Equal(t, int64(300), cpuMax, "cpu max should be highest hourly value")
		assert.Equal(t, int64(1500), memMax, "mem max should be highest hourly value")
	}
}

func TestAddRowWeighted_DropsZeroAndScalesUsageNotCapacity(t *testing.T) {
	interval := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	row := MetricRow{
		IntervalStart: interval, Node: "node-a", Pod: "pod-a",
		CPUUsageMC: 100, MemUsageKiB: 200, CPURequestMC: 400, MemRequestKiB: 800,
		NodeCapacityCPUMC: 8000, NodeAllocatableCPUMC: 7000,
	}

	dropped := newNodeDayAccumulator()
	dropped.AddRowWeighted(row, 0)
	_, _, _, _, _, _, _, _, _, sampleCount := dropped.Finalize()
	assert.Equal(t, int64(0), sampleCount)

	half := newNodeDayAccumulator()
	half.AddRowWeighted(row, 0.5)
	cpuP50, _, _, memP50, _, _, maxCPUReq, maxMemReq, _, n := half.Finalize()
	assert.Equal(t, int64(1), n)
	assert.Equal(t, int64(50), cpuP50)
	assert.Equal(t, int64(100), memP50)
	assert.Equal(t, int64(200), maxCPUReq)
	assert.Equal(t, int64(400), maxMemReq)
	assert.Equal(t, int64(7000), half.MaxCPUAllocatableMC)
	assert.Equal(t, int64(8000), half.MaxCPUCapacityMC)
}
