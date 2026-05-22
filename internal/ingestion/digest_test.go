package ingestion

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
)

func TestComputeDigest(t *testing.T) {
	t.Run("single value", func(t *testing.T) {
		d := ComputeDigest([]int64{100})
		assert.Equal(t, int64(100), d.P50)
		assert.Equal(t, int64(100), d.P95)
		assert.Equal(t, int64(100), d.Max)
		assert.Equal(t, int64(100), d.Mean)
		assert.Equal(t, int64(1), d.Count)
	})

	t.Run("two values", func(t *testing.T) {
		d := ComputeDigest([]int64{100, 200})
		assert.Equal(t, int64(100), d.P50)
		assert.Equal(t, int64(100), d.P95)
		assert.Equal(t, int64(200), d.Max)
		assert.Equal(t, int64(150), d.Mean)
		assert.Equal(t, int64(2), d.Count)
	})

	t.Run("three values", func(t *testing.T) {
		d := ComputeDigest([]int64{100, 200, 300})
		assert.Equal(t, int64(200), d.P50)
		assert.Equal(t, int64(200), d.P95)
		assert.Equal(t, int64(200), d.P99)
		assert.Equal(t, int64(300), d.Max)
		assert.Equal(t, int64(200), d.Mean)
		assert.Equal(t, int64(3), d.Count)
	})

	t.Run("96 values (typical daily count)", func(t *testing.T) {
		values := make([]int64, 96)
		for i := range values {
			values[i] = int64(i + 1) // 1..96
		}
		d := ComputeDigest(values)
		assert.Equal(t, int64(48), d.P50) // rank 48
		assert.Equal(t, int64(91), d.P95) // rank 91.2 → 91
		assert.Equal(t, int64(94), d.P98) // rank 94.08 → 94
		assert.Equal(t, int64(95), d.P99) // rank 95.04 → 95
		assert.Equal(t, int64(96), d.Max)
		assert.Equal(t, int64(96), d.Count)
	})

	t.Run("empty input returns zero digest", func(t *testing.T) {
		d := ComputeDigest(nil)
		assert.Equal(t, int64(0), d.P50)
		assert.Equal(t, int64(0), d.Max)
		assert.Equal(t, int64(0), d.Count)
	})

	t.Run("all same values", func(t *testing.T) {
		values := make([]int64, 50)
		for i := range values {
			values[i] = 500
		}
		d := ComputeDigest(values)
		assert.Equal(t, int64(500), d.P50)
		assert.Equal(t, int64(500), d.P95)
		assert.Equal(t, int64(500), d.Max)
		assert.Equal(t, int64(500), d.Mean)
	})
}

func TestGroupCSVRows(t *testing.T) {
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	rows := []MetricRow{
		{IntervalStart: base, IntervalEnd: base.Add(15 * time.Minute), Namespace: "ns1", WorkloadName: "deploy1", WorkloadType: "deployment", ContainerName: "main", CPUUsageMC: 100, MemUsageKiB: 1024},
		{IntervalStart: base.Add(15 * time.Minute), IntervalEnd: base.Add(30 * time.Minute), Namespace: "ns1", WorkloadName: "deploy1", WorkloadType: "deployment", ContainerName: "main", CPUUsageMC: 200, MemUsageKiB: 2048},
		{IntervalStart: base, IntervalEnd: base.Add(15 * time.Minute), Namespace: "ns1", WorkloadName: "deploy1", WorkloadType: "deployment", ContainerName: "sidecar", CPUUsageMC: 50, MemUsageKiB: 512},
		{IntervalStart: base.Add(24 * time.Hour), IntervalEnd: base.Add(24*time.Hour + 15*time.Minute), Namespace: "ns1", WorkloadName: "deploy1", WorkloadType: "deployment", ContainerName: "main", CPUUsageMC: 300, MemUsageKiB: 3072},
	}

	groups := GroupCSVRows(rows, "org1", "cluster-uuid-1")

	t.Run("correct number of groups", func(t *testing.T) {
		assert.Len(t, groups, 3)
	})

	t.Run("main container day 1 has 2 rows", func(t *testing.T) {
		key := DigestKey{
			OrgID: "org1", ClusterUUID: "cluster-uuid-1",
			Namespace: "ns1", Workload: "deploy1", WorkloadType: "deployment",
			ContainerName: "main", BucketDate: base, ScheduleType: ScheduleTypeAllHours,
		}
		group, ok := groups[key]
		require.True(t, ok)
		assert.Len(t, group, 2)
	})

	t.Run("sidecar container has 1 row", func(t *testing.T) {
		key := DigestKey{
			OrgID: "org1", ClusterUUID: "cluster-uuid-1",
			Namespace: "ns1", Workload: "deploy1", WorkloadType: "deployment",
			ContainerName: "sidecar", BucketDate: base, ScheduleType: ScheduleTypeAllHours,
		}
		group, ok := groups[key]
		require.True(t, ok)
		assert.Len(t, group, 1)
	})

	t.Run("day 2 has 1 row", func(t *testing.T) {
		day2 := base.AddDate(0, 0, 1)
		key := DigestKey{
			OrgID: "org1", ClusterUUID: "cluster-uuid-1",
			Namespace: "ns1", Workload: "deploy1", WorkloadType: "deployment",
			ContainerName: "main", BucketDate: day2, ScheduleType: ScheduleTypeAllHours,
		}
		group, ok := groups[key]
		require.True(t, ok)
		assert.Len(t, group, 1)
	})
}

func TestComputePodCounts_WithWorkloadPodCount(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

	t.Run("single hour single row", func(t *testing.T) {
		rows := []MetricRow{
			{IntervalStart: base, WorkloadPodCount: 3},
		}
		pcMin, pcMax, pcAvg := computePodCounts(rows)
		assert.Equal(t, int64(3), pcMin)
		assert.Equal(t, int64(3), pcMax)
		assert.Equal(t, int64(3), pcAvg)
	})

	t.Run("multiple rows same hour takes max", func(t *testing.T) {
		rows := []MetricRow{
			{IntervalStart: base, WorkloadPodCount: 2},
			{IntervalStart: base.Add(5 * time.Minute), WorkloadPodCount: 5},
			{IntervalStart: base.Add(10 * time.Minute), WorkloadPodCount: 3},
		}
		pcMin, pcMax, pcAvg := computePodCounts(rows)
		assert.Equal(t, int64(5), pcMin)
		assert.Equal(t, int64(5), pcMax)
		assert.Equal(t, int64(5), pcAvg)
	})

	t.Run("two hours with different counts", func(t *testing.T) {
		rows := []MetricRow{
			{IntervalStart: base, WorkloadPodCount: 2},
			{IntervalStart: base.Add(time.Hour), WorkloadPodCount: 6},
		}
		pcMin, pcMax, pcAvg := computePodCounts(rows)
		assert.Equal(t, int64(2), pcMin)
		assert.Equal(t, int64(6), pcMax)
		assert.Equal(t, int64(4), pcAvg)
	})

	t.Run("three hours", func(t *testing.T) {
		rows := []MetricRow{
			{IntervalStart: base, WorkloadPodCount: 1},
			{IntervalStart: base.Add(time.Hour), WorkloadPodCount: 2},
			{IntervalStart: base.Add(2 * time.Hour), WorkloadPodCount: 3},
		}
		pcMin, pcMax, pcAvg := computePodCounts(rows)
		assert.Equal(t, int64(1), pcMin)
		assert.Equal(t, int64(3), pcMax)
		assert.Equal(t, int64(2), pcAvg)
	})

	t.Run("empty rows returns zeros", func(t *testing.T) {
		pcMin, pcMax, pcAvg := computePodCounts(nil)
		assert.Equal(t, int64(0), pcMin)
		assert.Equal(t, int64(0), pcMax)
		assert.Equal(t, int64(0), pcAvg)
	})
}

func TestComputePodCounts_FallbackDistinctPods(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

	t.Run("distinct pods in same hour", func(t *testing.T) {
		rows := []MetricRow{
			{IntervalStart: base, Pod: "pod-a"},
			{IntervalStart: base.Add(5 * time.Minute), Pod: "pod-b"},
			{IntervalStart: base.Add(10 * time.Minute), Pod: "pod-a"},
		}
		pcMin, pcMax, pcAvg := computePodCounts(rows)
		assert.Equal(t, int64(2), pcMin)
		assert.Equal(t, int64(2), pcMax)
		assert.Equal(t, int64(2), pcAvg)
	})

	t.Run("distinct pods across two hours", func(t *testing.T) {
		rows := []MetricRow{
			{IntervalStart: base, Pod: "pod-a"},
			{IntervalStart: base, Pod: "pod-b"},
			{IntervalStart: base, Pod: "pod-c"},
			{IntervalStart: base.Add(time.Hour), Pod: "pod-a"},
		}
		pcMin, pcMax, pcAvg := computePodCounts(rows)
		assert.Equal(t, int64(1), pcMin)
		assert.Equal(t, int64(3), pcMax)
		assert.Equal(t, int64(2), pcAvg)
	})

	t.Run("no pods and no workload_pod_count returns zeros", func(t *testing.T) {
		rows := []MetricRow{
			{IntervalStart: base},
		}
		pcMin, pcMax, pcAvg := computePodCounts(rows)
		assert.Equal(t, int64(0), pcMin)
		assert.Equal(t, int64(0), pcMax)
		assert.Equal(t, int64(0), pcAvg)
	})
}

func TestComputeReplicaCounts(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

	t.Run("returns latest hour values", func(t *testing.T) {
		rows := []MetricRow{
			{IntervalStart: base, DesiredReplicas: 3, AvailableReplicas: 3},
			{IntervalStart: base.Add(time.Hour), DesiredReplicas: 5, AvailableReplicas: 4},
		}
		desired, available := computeReplicaCounts(rows)
		assert.Equal(t, int64(5), desired)
		assert.Equal(t, int64(4), available)
	})

	t.Run("takes max within same hour", func(t *testing.T) {
		rows := []MetricRow{
			{IntervalStart: base, DesiredReplicas: 2, AvailableReplicas: 1},
			{IntervalStart: base.Add(5 * time.Minute), DesiredReplicas: 4, AvailableReplicas: 3},
		}
		desired, available := computeReplicaCounts(rows)
		assert.Equal(t, int64(4), desired)
		assert.Equal(t, int64(3), available)
	})

	t.Run("empty rows returns zeros", func(t *testing.T) {
		desired, available := computeReplicaCounts(nil)
		assert.Equal(t, int64(0), desired)
		assert.Equal(t, int64(0), available)
	})

	t.Run("all zeros returns zeros", func(t *testing.T) {
		rows := []MetricRow{
			{IntervalStart: base, DesiredReplicas: 0, AvailableReplicas: 0},
		}
		desired, available := computeReplicaCounts(rows)
		assert.Equal(t, int64(0), desired)
		assert.Equal(t, int64(0), available)
	})
}

func TestMinMaxAvgOfMap(t *testing.T) {
	t.Run("single entry", func(t *testing.T) {
		m := map[hourKey]int64{{2026, 3, 1, 10}: 5}
		mn, mx, avg := minMaxAvgOfMap(m)
		assert.Equal(t, int64(5), mn)
		assert.Equal(t, int64(5), mx)
		assert.Equal(t, int64(5), avg)
	})

	t.Run("multiple entries", func(t *testing.T) {
		m := map[hourKey]int64{
			{2026, 3, 1, 10}: 2,
			{2026, 3, 1, 11}: 4,
			{2026, 3, 1, 12}: 6,
		}
		mn, mx, avg := minMaxAvgOfMap(m)
		assert.Equal(t, int64(2), mn)
		assert.Equal(t, int64(6), mx)
		assert.Equal(t, int64(4), avg)
	})

	t.Run("empty map", func(t *testing.T) {
		mn, mx, avg := minMaxAvgOfMap(nil)
		assert.Equal(t, int64(0), mn)
		assert.Equal(t, int64(0), mx)
		assert.Equal(t, int64(0), avg)
	})
}

func TestComputeWeightedDigest_KnownFixture(t *testing.T) {
	// Values 10..20 with weights 1.0 on 10..15 and 0.2 on 16..20.
	// Cumulative-weight p95 target 0.95 * 7.0 = 6.65 → first value with cum >= 6.65 is 18.
	values := []int64{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	weights := []float64{1, 1, 1, 1, 1, 1, 0.2, 0.2, 0.2, 0.2, 0.2}
	d := ComputeWeightedDigest(values, weights)
	assert.Equal(t, int64(19), d.P95)
}

func TestComputeWeightedDigest_MatchesUnweightedWhenAllOnes(t *testing.T) {
	values := make([]int64, 96)
	weights := make([]float64, 96)
	for i := range values {
		values[i] = int64(i + 1)
		weights[i] = 1.0
	}
	unweighted := ComputeDigest(append([]int64(nil), values...))
	weighted := ComputeWeightedDigest(values, weights)
	assert.Equal(t, unweighted, weighted)
}

func TestComputeWeightedDigest_BimodalOffHoursWeight(t *testing.T) {
	values := make([]int64, 96)
	for i := range values {
		values[i] = 100
	}
	values[95] = 10_000
	allWeights := make([]float64, len(values))
	for i := range allWeights {
		allWeights[i] = 1.0
	}
	allHours := ComputeWeightedDigest(values, allWeights)

	bhWeights := make([]float64, len(values))
	for i := range bhWeights {
		bhWeights[i] = 1.0
	}
	bhWeights[95] = 0.2
	businessHours := ComputeWeightedDigest(values, bhWeights)

	assert.Equal(t, int64(100), businessHours.P95)
	assert.Equal(t, int64(10_000), allHours.Max)
	assert.Greater(t, allHours.Max, businessHours.P95)
}

func TestGroupCSVRows_AllHours(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	rows := []MetricRow{
		{IntervalStart: base, Namespace: "ns1", WorkloadName: "wl", WorkloadType: "deployment", ContainerName: "c1", CPUUsageMC: 100},
		{IntervalStart: base.Add(15 * time.Minute), Namespace: "ns1", WorkloadName: "wl", WorkloadType: "deployment", ContainerName: "c1", CPUUsageMC: 200},
	}
	groups := GroupCSVRows(rows, "org1", "cluster-1")
	require.Len(t, groups, 1)
	for k := range groups {
		assert.Equal(t, ScheduleTypeAllHours, k.ScheduleType)
		assert.Len(t, groups[k], 2)
	}
}

func TestGroupCSVRows_BusinessHours_ParallelGroups(t *testing.T) {
	day := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC) // Tuesday
	inBH := day.Add(15 * time.Hour)                    // Tue 15:00 UTC = 10:00 NY
	offHours := day.Add(23 * time.Hour)                // Tue 23:00 UTC = 18:00 NY, outside BH window
	rows := []MetricRow{
		{IntervalStart: inBH, Namespace: "ns1", WorkloadName: "wl", WorkloadType: "deployment", ContainerName: "c1", CPUUsageMC: 100},
		{IntervalStart: offHours, Namespace: "ns1", WorkloadName: "wl", WorkloadType: "deployment", ContainerName: "c1", CPUUsageMC: 900},
	}
	sched := weekdaySchedule()
	weightFn := BusinessHoursRowWeightFn(sched)
	bhGroups := GroupCSVRowsForStream(rows, "org1", "cluster-1", ScheduleTypeBusinessHours, weightFn)
	allGroups := GroupCSVRows(rows, "org1", "cluster-1")
	require.Len(t, allGroups, 1)
	require.Len(t, bhGroups, 1)
	for k := range bhGroups {
		assert.Equal(t, ScheduleTypeBusinessHours, k.ScheduleType)
		assert.Len(t, bhGroups[k], 1, "off-hours row skipped when off_hours_weight=0")
	}
}

func TestGroupCSVRows_ScheduleDisabled_OnlyAllHours(t *testing.T) {
	rows := []MetricRow{
		{IntervalStart: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC), Namespace: "ns1", WorkloadName: "wl", WorkloadType: "deployment", ContainerName: "c1"},
	}
	cache := &bhschedule.Cache{}
	bh := buildBusinessHoursGroups(rows, "org1", "cluster-1", cache)
	assert.Empty(t, bh)
}

func TestGroupCSVRows_EffectiveDisabled_NoBHGroups(t *testing.T) {
	sched := weekdaySchedule()
	sched.Enabled = false
	weightFn := BusinessHoursRowWeightFn(sched)
	assert.Nil(t, weightFn)
}

func TestGroupCSVRows_OffHoursWeight02_IncludesOffHours(t *testing.T) {
	day := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)
	inBH := day.Add(15 * time.Hour)
	offHours := day.Add(20 * time.Hour)
	rows := []MetricRow{
		{IntervalStart: inBH, Namespace: "ns1", WorkloadName: "wl", WorkloadType: "deployment", ContainerName: "c1"},
		{IntervalStart: offHours, Namespace: "ns1", WorkloadName: "wl", WorkloadType: "deployment", ContainerName: "c1"},
	}
	sched := weekdaySchedule()
	sched.OffHoursWeight = 0.2
	weightFn := BusinessHoursRowWeightFn(sched)
	bhGroups := GroupCSVRowsForStream(rows, "org1", "cluster-1", ScheduleTypeBusinessHours, weightFn)
	require.Len(t, bhGroups, 1)
	for _, g := range bhGroups {
		assert.Len(t, g, 2)
	}
}

func TestAllHoursStream_IdenticalToPreFeature(t *testing.T) {
	rows := []MetricRow{
		{IntervalStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), CPUUsageMC: 80, CPURequestMC: 100, MemUsageKiB: 1000, MemRequestKiB: 2000},
		{IntervalStart: time.Date(2026, 4, 1, 0, 15, 0, 0, time.UTC), CPUUsageMC: 90, CPURequestMC: 100, MemUsageKiB: 1100, MemRequestKiB: 2000},
		{IntervalStart: time.Date(2026, 4, 1, 0, 30, 0, 0, time.UTC), CPUUsageMC: 100, CPURequestMC: 100, MemUsageKiB: 1200, MemRequestKiB: 2000},
	}
	key := DigestKey{
		OrgID: "org1", ClusterUUID: "cluster-1",
		Namespace: "ns", Workload: "wl", WorkloadType: "deployment",
		ContainerName: "main", BucketDate: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		ScheduleType: ScheduleTypeAllHours,
	}
	legacy := ComputeContainerDigest(key, rows)
	current := ComputeContainerDigestWeighted(key, rows, nil)
	assert.Equal(t, legacy, current)
}

func TestOffHoursWeight0_NoSortOverhead(t *testing.T) {
	sched := weekdaySchedule()
	sched.OffHoursWeight = 0.0
	weightFn := BusinessHoursRowWeightFn(sched)
	off := time.Date(2026, 1, 10, 15, 0, 0, 0, time.UTC)
	row := MetricRow{IntervalStart: off}
	assert.Equal(t, 0.0, weightFn(row))
}

