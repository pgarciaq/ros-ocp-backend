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

func TestAggregateNodeDigests_AccumulatesPerInterval(t *testing.T) {
	interval := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	// Two containers on the same node at the same interval
	rows := []MetricRow{
		{IntervalStart: interval, Node: "node-a", CPUUsageMC: 100, MemUsageKiB: 500, CPURequestMC: 200, MemRequestKiB: 1000},
		{IntervalStart: interval, Node: "node-a", CPUUsageMC: 150, MemUsageKiB: 600, CPURequestMC: 300, MemRequestKiB: 2000},
	}

	result := AggregateNodeDigests(rows)
	require.Len(t, result, 1)

	for _, acc := range result {
		cpuP50, cpuP95, memP50, memP95, maxCPUReq, maxMemReq, maxPods, sampleCount := acc.Finalize()

		// Both containers on same interval → single sample with sum = 250 CPU, 1100 mem
		assert.Equal(t, int64(250), cpuP50)
		assert.Equal(t, int64(250), cpuP95) // only 1 sample, so p50 == p95
		assert.Equal(t, int64(1100), memP50)
		assert.Equal(t, int64(1100), memP95)
		assert.Equal(t, int64(500), maxCPUReq) // 200 + 300
		assert.Equal(t, int64(3000), maxMemReq)
		assert.Equal(t, int64(2), maxPods)
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
		cpuP50, cpuP95, memP50, memP95, _, _, _, sampleCount := acc.Finalize()
		assert.Equal(t, int64(10), sampleCount)
		// Sorted: [100,200,300,400,500,600,700,800,900,1000]
		// P50 index: int(0.50 * 9) = 4 → 500
		assert.Equal(t, int64(500), cpuP50)
		// P95 index: int(0.95 * 9) = 8 → 900
		assert.Equal(t, int64(900), cpuP95)
		assert.Equal(t, int64(5000), memP50)
		assert.Equal(t, int64(9000), memP95)
	}
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

func TestPercentileInt64(t *testing.T) {
	assert.Equal(t, int64(0), percentileInt64(nil, 0.5))
	assert.Equal(t, int64(0), percentileInt64([]int64{}, 0.5))
	assert.Equal(t, int64(42), percentileInt64([]int64{42}, 0.5))

	sorted := []int64{10, 20, 30, 40, 50}
	assert.Equal(t, int64(30), percentileInt64(sorted, 0.50))
	assert.Equal(t, int64(40), percentileInt64(sorted, 0.75))
	assert.Equal(t, int64(40), percentileInt64(sorted, 0.95))
}
