package ingestion

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			ContainerName: "main", BucketDate: base,
		}
		group, ok := groups[key]
		require.True(t, ok)
		assert.Len(t, group, 2)
	})

	t.Run("sidecar container has 1 row", func(t *testing.T) {
		key := DigestKey{
			OrgID: "org1", ClusterUUID: "cluster-uuid-1",
			Namespace: "ns1", Workload: "deploy1", WorkloadType: "deployment",
			ContainerName: "sidecar", BucketDate: base,
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
			ContainerName: "main", BucketDate: day2,
		}
		group, ok := groups[key]
		require.True(t, ok)
		assert.Len(t, group, 1)
	})
}
