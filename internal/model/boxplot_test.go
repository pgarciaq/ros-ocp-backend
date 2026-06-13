package model

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestPlotDetails_JSONFields(t *testing.T) {
	pd := PlotDetails{P50: 0.5, P95: 0.9, P99: 0.99, Max: 1.0, Format: "cores"}
	assert.Equal(t, 0.5, pd.P50)
	assert.Equal(t, 0.9, pd.P95)
	assert.Equal(t, 0.99, pd.P99)
	assert.Equal(t, 1.0, pd.Max)
	assert.Equal(t, "cores", pd.Format)
}

func TestAssembleBoxplots_ShortTerm_DailyBuckets(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := ContainerKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload,
		WorkloadType: testutil.TestWorkloadType, ContainerName: testutil.TestContainer,
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
		BucketDate: today, OrgID: key.OrgID, ClusterUUID: key.ClusterUUID,
		Namespace: key.Namespace, Workload: key.Workload, WorkloadType: key.WorkloadType,
		ContainerName: key.ContainerName,
		CPUUsageP50MC: 990, CPUUsageP95MC: 1000, CPUUsageP99MC: 1008, CPUUsageMaxMC: 1015,
		MemUsageP50KiB: 2048 * 1024, MemUsageP95KiB: 2048*1024 + 512,
		MemUsageP99KiB: 2048*1024 + 900, MemUsageMaxKiB: 2048*1024 + 1024,
	})

	plot, err := AssembleBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)
	assert.Equal(t, 1, plot.DataPoints, "short_term 24h window should have one daily bucket")

	for _, pd := range plot.PlotsData {
		require.NotNil(t, pd.CPUUsage)
		require.NotNil(t, pd.MemoryUsage)
		assert.Equal(t, "cores", pd.CPUUsage.Format)
		assert.Equal(t, "MiB", pd.MemoryUsage.Format)
		assert.LessOrEqual(t, pd.CPUUsage.P50, pd.CPUUsage.P95)
		assert.LessOrEqual(t, pd.CPUUsage.P95, pd.CPUUsage.P99)
		assert.LessOrEqual(t, pd.CPUUsage.P99, pd.CPUUsage.Max)
	}
}

func TestAssembleBoxplots_MediumTerm_DailyWindows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := ContainerKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-med", Workload: "deploy-med", WorkloadType: testutil.TestWorkloadType,
		ContainerName: "main",
	}

	for d := 0; d < 7; d++ {
		bucketDate := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -(6 - d))
		cpuVal := int64(200 + d*10)
		memVal := int64(50000 + d*1000)
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate: bucketDate, OrgID: key.OrgID, ClusterUUID: key.ClusterUUID,
			Namespace: key.Namespace, Workload: key.Workload, WorkloadType: key.WorkloadType,
			ContainerName: key.ContainerName,
			CPUUsageP50MC: cpuVal - 10, CPUUsageP95MC: cpuVal, CPUUsageP99MC: cpuVal + 8,
			CPUUsageMaxMC: cpuVal + 15,
			MemUsageP50KiB: memVal - 512, MemUsageP95KiB: memVal, MemUsageP99KiB: memVal + 900,
			MemUsageMaxKiB: memVal + 1024,
		})
	}

	plot, err := AssembleBoxplots(ctx, pool, key, "medium_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)
	assert.GreaterOrEqual(t, plot.DataPoints, 6, "medium_term should have ~7 daily buckets")
}

func TestAssembleBoxplots_NoData_ReturnsNil(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := ContainerKey{
		OrgID: "no-such-org", ClusterUUID: testutil.TestClusterUUID,
		Namespace: "none", Workload: "none", WorkloadType: testutil.TestWorkloadType,
		ContainerName: "none",
	}

	plot, err := AssembleBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	assert.Nil(t, plot, "no data should return nil plot")
}

func TestAssembleBoxplots_UnitConversion(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := ContainerKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-unit", Workload: "deploy-unit", WorkloadType: testutil.TestWorkloadType,
		ContainerName: "main",
	}

	bucketDate := time.Now().UTC().Truncate(24 * time.Hour)
	testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
		BucketDate: bucketDate, OrgID: key.OrgID, ClusterUUID: key.ClusterUUID,
		Namespace: key.Namespace, Workload: key.Workload, WorkloadType: key.WorkloadType,
		ContainerName: key.ContainerName,
		CPUUsageP50MC: 1000, CPUUsageP95MC: 1100, CPUUsageP99MC: 1150, CPUUsageMaxMC: 1200,
		MemUsageP50KiB: 2048, MemUsageP95KiB: 2100, MemUsageP99KiB: 2150, MemUsageMaxKiB: 2200,
	})

	plot, err := AssembleBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)
	require.Equal(t, 1, plot.DataPoints)

	for _, pd := range plot.PlotsData {
		assert.InDelta(t, 1.0, pd.CPUUsage.P50, 0.001, "1000mc should be 1.0 cores")
		assert.InDelta(t, 2.0, pd.MemoryUsage.P50, 0.001, "2048 KiB should be 2.0 MiB")
	}
}

func TestMonitoringEndTime(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := ContainerKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload,
		ContainerName: testutil.TestContainer,
	}

	testutil.SeedDigestSeries(t, pool, 5, 100, 10, 10000, 100)

	met, err := MonitoringEndTime(ctx, pool, key)
	require.NoError(t, err)
	assert.False(t, met.IsZero(), "should return a non-zero date")
}

func TestMonitoringEndTime_NoData(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := ContainerKey{
		OrgID: "no-data-org", ClusterUUID: testutil.TestClusterUUID,
		Namespace: "none", Workload: "none", ContainerName: "none",
	}

	met, err := MonitoringEndTime(ctx, pool, key)
	require.NoError(t, err)
	assert.True(t, met.IsZero() || met.Year() == 1, "no data should return zero-equivalent time")
}

func TestAssembleNamespaceBoxplots_ShortTerm_DailyBuckets(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: testutil.TestNamespace,
	}

	// Seed today's bucket so short_term 24h window includes it.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	testutil.EnsureMonthlyPartition(t, pool, "daily_namespace_digests", today)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO daily_namespace_digests (
			bucket_date, org_id, cluster_uuid, namespace,
			cpu_usage_p50_mc, cpu_usage_p95_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
			memory_usage_p50_kib, memory_usage_p95_kib, memory_usage_p99_kib, memory_usage_max_kib,
			cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (org_id, cluster_uuid, namespace, bucket_date, schedule_type) DO UPDATE SET
			cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc`,
		today, key.OrgID, key.ClusterUUID, key.Namespace,
		int64(90), int64(100), int64(108), int64(115),
		int64(9900), int64(10000), int64(10900), int64(11000),
		int64(95), int64(9950), int64(96),
	)
	require.NoError(t, err)

	plot, err := AssembleNamespaceBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)
	assert.Equal(t, 1, plot.DataPoints)

	for _, pd := range plot.PlotsData {
		require.NotNil(t, pd.CPUUsage)
		require.NotNil(t, pd.MemoryUsage)
		assert.Equal(t, "cores", pd.CPUUsage.Format)
		assert.Equal(t, "MiB", pd.MemoryUsage.Format)
		assert.LessOrEqual(t, pd.CPUUsage.P50, pd.CPUUsage.P95)
		assert.LessOrEqual(t, pd.CPUUsage.P95, pd.CPUUsage.P99)
		assert.LessOrEqual(t, pd.CPUUsage.P99, pd.CPUUsage.Max)
	}
}

func TestAssembleNamespaceBoxplots_MediumTerm_DailyWindows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-med-ns",
	}

	start := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -6)
	for d := 0; d < 7; d++ {
		bucketDate := start.AddDate(0, 0, d)
		cpuVal := int64(200 + d*10)
		memVal := int64(50000 + d*1000)
		testutil.EnsureMonthlyPartition(t, pool, "daily_namespace_digests", bucketDate)
		_, err := pool.Exec(context.Background(), `
			INSERT INTO daily_namespace_digests (
				bucket_date, org_id, cluster_uuid, namespace,
				cpu_usage_p50_mc, cpu_usage_p60_mc, cpu_usage_p95_mc, cpu_usage_p98_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
				memory_usage_p50_kib, memory_usage_p60_kib, memory_usage_p95_kib, memory_usage_p98_kib, memory_usage_p99_kib, memory_usage_max_kib,
				cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
			ON CONFLICT (org_id, cluster_uuid, namespace, bucket_date, schedule_type) DO UPDATE SET
				cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc`,
			bucketDate, key.OrgID, key.ClusterUUID, key.Namespace,
			cpuVal-10, cpuVal, cpuVal+10, cpuVal+15, cpuVal+18, cpuVal+25,
			memVal-512, memVal, memVal+512, memVal+768, memVal+900, memVal+1024,
			cpuVal-5, memVal-256, int64(96),
		)
		require.NoError(t, err)
	}

	plot, err := AssembleNamespaceBoxplots(ctx, pool, key, "medium_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)
	assert.GreaterOrEqual(t, plot.DataPoints, 6, "medium_term should have ~7 daily buckets")
}

func TestAssembleNamespaceBoxplots_LongTerm_15Days(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-long-ns",
	}

	start := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -14)
	for d := 0; d < 15; d++ {
		bucketDate := start.AddDate(0, 0, d)
		cpuVal := int64(300 + d*5)
		memVal := int64(80000 + d*500)
		testutil.EnsureMonthlyPartition(t, pool, "daily_namespace_digests", bucketDate)
		_, err := pool.Exec(context.Background(), `
			INSERT INTO daily_namespace_digests (
				bucket_date, org_id, cluster_uuid, namespace,
				cpu_usage_p50_mc, cpu_usage_p60_mc, cpu_usage_p95_mc, cpu_usage_p98_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
				memory_usage_p50_kib, memory_usage_p60_kib, memory_usage_p95_kib, memory_usage_p98_kib, memory_usage_p99_kib, memory_usage_max_kib,
				cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
			ON CONFLICT (org_id, cluster_uuid, namespace, bucket_date, schedule_type) DO UPDATE SET
				cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc`,
			bucketDate, key.OrgID, key.ClusterUUID, key.Namespace,
			cpuVal-10, cpuVal, cpuVal+10, cpuVal+15, cpuVal+18, cpuVal+25,
			memVal-512, memVal, memVal+512, memVal+768, memVal+900, memVal+1024,
			cpuVal-5, memVal-256, int64(96),
		)
		require.NoError(t, err)
	}

	plot, err := AssembleNamespaceBoxplots(ctx, pool, key, "long_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)
	assert.GreaterOrEqual(t, plot.DataPoints, 14, "long_term should have ~15 daily buckets")
}

func TestAssembleNamespaceBoxplots_NoData_ReturnsNil(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := NamespaceKey{
		OrgID: "no-such-org", ClusterUUID: testutil.TestClusterUUID,
		Namespace: "none",
	}

	plot, err := AssembleNamespaceBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	assert.Nil(t, plot, "no data should return nil plot")
}

func TestAssembleNamespaceBoxplots_UnknownTerm_ReturnsError(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: testutil.TestNamespace,
	}

	plot, err := AssembleNamespaceBoxplots(ctx, pool, key, "extra_long_term", key.OrgID)
	assert.Error(t, err, "unknown term should error")
	assert.Nil(t, plot)
	assert.Contains(t, err.Error(), "unknown term")
}

func TestAssembleNamespaceBoxplots_UnitConversion(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-unit-conv",
	}

	bucketDate := time.Now().UTC().Truncate(24 * time.Hour)
	testutil.EnsureMonthlyPartition(t, pool, "daily_namespace_digests", bucketDate)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO daily_namespace_digests (
			bucket_date, org_id, cluster_uuid, namespace,
			cpu_usage_p50_mc, cpu_usage_p95_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
			memory_usage_p50_kib, memory_usage_p95_kib, memory_usage_p99_kib, memory_usage_max_kib,
			cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (org_id, cluster_uuid, namespace, bucket_date, schedule_type) DO UPDATE SET
			cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc`,
		bucketDate, key.OrgID, key.ClusterUUID, key.Namespace,
		int64(1000), int64(1100), int64(1150), int64(1200),
		int64(2048), int64(2100), int64(2150), int64(2200),
		int64(990), int64(2000), int64(96),
	)
	require.NoError(t, err)

	plot, err := AssembleNamespaceBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)
	require.Equal(t, 1, plot.DataPoints)

	for _, pd := range plot.PlotsData {
		assert.InDelta(t, 1.0, pd.CPUUsage.P50, 0.001, "1000mc should be 1.0 cores")
		assert.InDelta(t, 2.0, pd.MemoryUsage.P50, 0.001, "2048 KiB should be 2.0 MiB")
	}
}

func TestAssembleNamespaceBoxplots_PercentileOrdering(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-ordering",
	}

	testutil.SeedNamespaceDigestSeriesFull(t, pool, "ns-ordering", 3, 50, 5, 5000, 100)
	// Ensure at least one bucket is inside the short_term window.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	testutil.EnsureMonthlyPartition(t, pool, "daily_namespace_digests", today)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO daily_namespace_digests (
			bucket_date, org_id, cluster_uuid, namespace,
			cpu_usage_p50_mc, cpu_usage_p60_mc, cpu_usage_p95_mc, cpu_usage_p98_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
			memory_usage_p50_kib, memory_usage_p60_kib, memory_usage_p95_kib, memory_usage_p98_kib, memory_usage_p99_kib, memory_usage_max_kib,
			cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		ON CONFLICT (org_id, cluster_uuid, namespace, bucket_date, schedule_type) DO NOTHING`,
		today, key.OrgID, key.ClusterUUID, key.Namespace,
		int64(50), int64(55), int64(60), int64(65), int64(68), int64(75),
		int64(5000), int64(5100), int64(5500), int64(5700), int64(5900), int64(6000),
		int64(52), int64(5050), int64(96),
	)
	require.NoError(t, err)

	plot, err := AssembleNamespaceBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)

	for _, pd := range plot.PlotsData {
		require.NotNil(t, pd.CPUUsage)
		require.NotNil(t, pd.MemoryUsage)
		assert.LessOrEqual(t, pd.CPUUsage.P50, pd.CPUUsage.P95)
		assert.LessOrEqual(t, pd.CPUUsage.P95, pd.CPUUsage.P99)
		assert.LessOrEqual(t, pd.CPUUsage.P99, pd.CPUUsage.Max)
		assert.LessOrEqual(t, pd.MemoryUsage.P50, pd.MemoryUsage.P95)
		assert.LessOrEqual(t, pd.MemoryUsage.P95, pd.MemoryUsage.P99)
		assert.LessOrEqual(t, pd.MemoryUsage.P99, pd.MemoryUsage.Max)
	}
}

func TestNamespaceMonitoringEndTime(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-met-test",
	}

	testutil.SeedNamespaceDigestSeries(t, pool, key.Namespace, 5, 100, 10, 10000, 100)

	met, err := NamespaceMonitoringEndTime(ctx, pool, key)
	require.NoError(t, err)
	assert.False(t, met.IsZero(), "should return a non-zero date")
}

func TestNamespaceMonitoringEndTime_NoData(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := NamespaceKey{
		OrgID: "no-data-org", ClusterUUID: testutil.TestClusterUUID,
		Namespace: "none",
	}

	met, err := NamespaceMonitoringEndTime(ctx, pool, key)
	require.NoError(t, err)
	assert.True(t, met.IsZero() || met.Year() == 1, "no data should return zero-equivalent time")
}
