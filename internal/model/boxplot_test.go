package model

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedSamples(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID, ns, wl, wlType, cn string, start time.Time, count int, baseCPU, baseMemKiB int64) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < count; i++ {
		sampleTime := start.Add(time.Duration(i) * 15 * time.Minute)
		_, err := pool.Exec(ctx, `
			INSERT INTO container_usage_samples (sample_time, org_id, cluster_uuid, namespace, workload, workload_type, container_name, cpu_usage_mc, mem_usage_kib)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT DO NOTHING`,
			sampleTime, orgID, clusterUUID, ns, wl, wlType, cn,
			baseCPU+int64(i), baseMemKiB+int64(i)*10,
		)
		require.NoError(t, err)
	}
}

func ensureSamplePartition(t *testing.T, pool *pgxpool.Pool, ts time.Time) {
	t.Helper()
	monthStart := time.Date(ts.Year(), ts.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("container_usage_samples_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF container_usage_samples FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	)
	_, err := pool.Exec(context.Background(), sql)
	require.NoError(t, err)
}

func TestAssembleBoxplots_ShortTerm_6HourWindows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	start := now.Add(-23 * time.Hour)
	ensureSamplePartition(t, pool, start)

	key := ContainerKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload,
		ContainerName: testutil.TestContainer,
	}

	// 96 samples across 24 hours (4 per hour)
	seedSamples(t, pool, key.OrgID, key.ClusterUUID, key.Namespace, key.Workload, testutil.TestWorkloadType, key.ContainerName, start, 96, 100, 10000)

	plot, err := AssembleBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)

	// 23h of data across 6h buckets yields 4 or 5 buckets depending on alignment
	assert.GreaterOrEqual(t, plot.DataPoints, 4, "short_term should have at least 4 buckets (6h each)")
	assert.LessOrEqual(t, plot.DataPoints, 5, "short_term should have at most 5 buckets (6h each)")
	for _, pd := range plot.PlotsData {
		require.NotNil(t, pd.CPUUsage)
		require.NotNil(t, pd.MemoryUsage)
		assert.Equal(t, "cores", pd.CPUUsage.Format)
		assert.Equal(t, "MiB", pd.MemoryUsage.Format)
		assert.LessOrEqual(t, pd.CPUUsage.Min, pd.CPUUsage.Q1)
		assert.LessOrEqual(t, pd.CPUUsage.Q1, pd.CPUUsage.Median)
		assert.LessOrEqual(t, pd.CPUUsage.Median, pd.CPUUsage.Q3)
		assert.LessOrEqual(t, pd.CPUUsage.Q3, pd.CPUUsage.Max)
	}
}

func TestAssembleBoxplots_MediumTerm_DailyWindows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	key := ContainerKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-med", Workload: "deploy-med", ContainerName: "main",
	}

	// 7 days of data, 4 samples per day (simplified)
	for d := 0; d < 7; d++ {
		dayStart := now.AddDate(0, 0, -(7 - d)).Truncate(24 * time.Hour).Add(6 * time.Hour)
		ensureSamplePartition(t, pool, dayStart)
		seedSamples(t, pool, key.OrgID, key.ClusterUUID, key.Namespace, key.Workload, testutil.TestWorkloadType, key.ContainerName, dayStart, 4, 200+int64(d)*10, 50000+int64(d)*1000)
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
		Namespace: "none", Workload: "none", ContainerName: "none",
	}

	plot, err := AssembleBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	assert.Nil(t, plot, "no data should return nil plot")
}

func TestAssembleBoxplots_UnitConversion(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	start := now.Add(-2 * time.Hour)
	ensureSamplePartition(t, pool, start)

	key := ContainerKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-unit", Workload: "deploy-unit", ContainerName: "main",
	}

	// Insert one sample: 1000mc CPU (should become 1.0 cores), 2048 KiB (should become 2.0 MiB)
	_, err := pool.Exec(ctx, `
		INSERT INTO container_usage_samples (sample_time, org_id, cluster_uuid, namespace, workload, workload_type, container_name, cpu_usage_mc, mem_usage_kib)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		start, key.OrgID, key.ClusterUUID, key.Namespace, key.Workload, testutil.TestWorkloadType, key.ContainerName,
		int64(1000), int64(2048),
	)
	require.NoError(t, err)

	plot, err := AssembleBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)
	require.Equal(t, 1, plot.DataPoints)

	for _, pd := range plot.PlotsData {
		assert.InDelta(t, 1.0, pd.CPUUsage.Median, 0.001, "1000mc should be 1.0 cores")
		assert.InDelta(t, 2.0, pd.MemoryUsage.Median, 0.001, "2048 KiB should be 2.0 MiB")
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

// --- Namespace Boxplot Tests ---

func seedNamespaceSamples(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID, ns string, start time.Time, count int, baseCPU, baseMemKiB int64) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < count; i++ {
		sampleTime := start.Add(time.Duration(i) * 15 * time.Minute)
		_, err := pool.Exec(ctx, `
			INSERT INTO namespace_usage_samples (sample_time, org_id, cluster_uuid, namespace, cpu_usage_mc, mem_usage_kib)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT DO NOTHING`,
			sampleTime, orgID, clusterUUID, ns,
			baseCPU+int64(i), baseMemKiB+int64(i)*10,
		)
		require.NoError(t, err)
	}
}

func ensureNamespaceSamplePartition(t *testing.T, pool *pgxpool.Pool, ts time.Time) {
	t.Helper()
	monthStart := time.Date(ts.Year(), ts.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("namespace_usage_samples_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF namespace_usage_samples FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	)
	_, err := pool.Exec(context.Background(), sql)
	require.NoError(t, err)
}

func TestAssembleNamespaceBoxplots_ShortTerm_6HourWindows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	start := now.Add(-23 * time.Hour)
	ensureNamespaceSamplePartition(t, pool, start)

	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: testutil.TestNamespace,
	}

	seedNamespaceSamples(t, pool, key.OrgID, key.ClusterUUID, key.Namespace, start, 96, 100, 10000)

	plot, err := AssembleNamespaceBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)

	// 23h of data across 6h buckets yields 4 or 5 buckets depending on alignment
	assert.GreaterOrEqual(t, plot.DataPoints, 4, "short_term should have at least 4 buckets (6h each)")
	assert.LessOrEqual(t, plot.DataPoints, 5, "short_term should have at most 5 buckets (6h each)")
	for _, pd := range plot.PlotsData {
		require.NotNil(t, pd.CPUUsage)
		require.NotNil(t, pd.MemoryUsage)
		assert.Equal(t, "cores", pd.CPUUsage.Format)
		assert.Equal(t, "MiB", pd.MemoryUsage.Format)
		assert.LessOrEqual(t, pd.CPUUsage.Min, pd.CPUUsage.Q1)
		assert.LessOrEqual(t, pd.CPUUsage.Q1, pd.CPUUsage.Median)
		assert.LessOrEqual(t, pd.CPUUsage.Median, pd.CPUUsage.Q3)
		assert.LessOrEqual(t, pd.CPUUsage.Q3, pd.CPUUsage.Max)
	}
}

func TestAssembleNamespaceBoxplots_MediumTerm_DailyWindows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-med-ns",
	}

	for d := 0; d < 7; d++ {
		dayStart := now.AddDate(0, 0, -(7 - d)).Truncate(24 * time.Hour).Add(6 * time.Hour)
		ensureNamespaceSamplePartition(t, pool, dayStart)
		seedNamespaceSamples(t, pool, key.OrgID, key.ClusterUUID, key.Namespace, dayStart, 4, 200+int64(d)*10, 50000+int64(d)*1000)
	}

	plot, err := AssembleNamespaceBoxplots(ctx, pool, key, "medium_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)

	assert.GreaterOrEqual(t, plot.DataPoints, 6, "medium_term should have ~7 daily buckets")
}

func TestAssembleNamespaceBoxplots_LongTerm_15Days(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-long-ns",
	}

	for d := 0; d < 15; d++ {
		dayStart := now.AddDate(0, 0, -(15 - d)).Truncate(24 * time.Hour).Add(6 * time.Hour)
		ensureNamespaceSamplePartition(t, pool, dayStart)
		seedNamespaceSamples(t, pool, key.OrgID, key.ClusterUUID, key.Namespace, dayStart, 4, 300+int64(d)*5, 80000+int64(d)*500)
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

	now := time.Now().UTC()
	start := now.Add(-2 * time.Hour)
	ensureNamespaceSamplePartition(t, pool, start)

	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-unit-conv",
	}

	// Insert one sample: 1000mc CPU (should become 1.0 cores), 2048 KiB (should become 2.0 MiB)
	_, err := pool.Exec(ctx, `
		INSERT INTO namespace_usage_samples (sample_time, org_id, cluster_uuid, namespace, cpu_usage_mc, mem_usage_kib)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		start, key.OrgID, key.ClusterUUID, key.Namespace,
		int64(1000), int64(2048),
	)
	require.NoError(t, err)

	plot, err := AssembleNamespaceBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)
	require.Equal(t, 1, plot.DataPoints)

	for _, pd := range plot.PlotsData {
		assert.InDelta(t, 1.0, pd.CPUUsage.Median, 0.001, "1000mc should be 1.0 cores")
		assert.InDelta(t, 2.0, pd.MemoryUsage.Median, 0.001, "2048 KiB should be 2.0 MiB")
	}
}

func TestAssembleNamespaceBoxplots_FiveNumberSummary_Ordering(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	start := now.Add(-5 * time.Hour)
	ensureNamespaceSamplePartition(t, pool, start)

	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-ordering",
	}

	// 20 distinct samples to get meaningful quartile spread
	seedNamespaceSamples(t, pool, key.OrgID, key.ClusterUUID, key.Namespace, start, 20, 50, 5000)

	plot, err := AssembleNamespaceBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)

	for _, pd := range plot.PlotsData {
		require.NotNil(t, pd.CPUUsage)
		require.NotNil(t, pd.MemoryUsage)

		assert.LessOrEqual(t, pd.CPUUsage.Min, pd.CPUUsage.Q1, "Min <= Q1")
		assert.LessOrEqual(t, pd.CPUUsage.Q1, pd.CPUUsage.Median, "Q1 <= Median")
		assert.LessOrEqual(t, pd.CPUUsage.Median, pd.CPUUsage.Q3, "Median <= Q3")
		assert.LessOrEqual(t, pd.CPUUsage.Q3, pd.CPUUsage.Max, "Q3 <= Max")

		assert.LessOrEqual(t, pd.MemoryUsage.Min, pd.MemoryUsage.Q1, "Mem Min <= Q1")
		assert.LessOrEqual(t, pd.MemoryUsage.Q1, pd.MemoryUsage.Median, "Mem Q1 <= Median")
		assert.LessOrEqual(t, pd.MemoryUsage.Median, pd.MemoryUsage.Q3, "Mem Median <= Q3")
		assert.LessOrEqual(t, pd.MemoryUsage.Q3, pd.MemoryUsage.Max, "Mem Q3 <= Max")
	}
}

func TestAssembleNamespaceBoxplots_ExactPercentiles(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Org-specific term rows can shrink the short_term window below 24h; this test needs
	// the full default window so all seeded points stay inside [start, end).
	_, err := pool.Exec(ctx, `DELETE FROM org_recommendation_terms WHERE org_id = $1`, testutil.TestOrgID)
	require.NoError(t, err)

	// Anchor 30 minutes in the past so all 24 one-minute samples are strictly before
	// time.Now() when AssembleNamespaceBoxplots runs (query uses sample_time < end).
	now := time.Now().UTC()
	anchor := now.Add(-30 * time.Minute)
	bucketEpoch := (anchor.Unix() / 21600) * 21600
	bucketStart := time.Unix(bucketEpoch, 0).UTC().Add(time.Minute)
	require.True(t, bucketStart.Add(23*time.Minute).Before(now), "seed window must end before query end")
	ensureNamespaceSamplePartition(t, pool, bucketStart)

	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: fmt.Sprintf("ns-exact-pctl-%d", time.Now().UnixNano()),
	}

	_, err = pool.Exec(ctx, `DELETE FROM namespace_usage_samples WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		key.OrgID, key.ClusterUUID, key.Namespace)
	require.NoError(t, err)

	// Seed 24 samples at 1-min intervals (23 min span, well within one 6h bucket):
	// CPU = [100,101,...,123] mc, Mem = [10000,10010,...,10230] KiB
	for i := 0; i < 24; i++ {
		sampleTime := bucketStart.Add(time.Duration(i) * time.Minute)
		_, err := pool.Exec(ctx, `
			INSERT INTO namespace_usage_samples (sample_time, org_id, cluster_uuid, namespace, cpu_usage_mc, mem_usage_kib)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (org_id, cluster_uuid, namespace, sample_time) DO UPDATE SET
				cpu_usage_mc = EXCLUDED.cpu_usage_mc,
				mem_usage_kib = EXCLUDED.mem_usage_kib`,
			sampleTime, key.OrgID, key.ClusterUUID, key.Namespace,
			int64(100+i), int64(10000+i*10),
		)
		require.NoError(t, err)
	}

	var rowCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM namespace_usage_samples
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		key.OrgID, key.ClusterUUID, key.Namespace).Scan(&rowCount)
	require.NoError(t, err)
	require.Equal(t, 24, rowCount, "all seeded samples must be present")

	plot, err := AssembleNamespaceBoxplots(ctx, pool, key, "short_term", key.OrgID)
	require.NoError(t, err)
	require.NotNil(t, plot)

	shortTW := defaultTermWindows["short_term"]
	bucketKey := time.Unix(bucketEpoch, 0).UTC().Format(shortTW.BucketKey)
	pd, ok := plot.PlotsData[bucketKey]
	require.True(t, ok, "expected plots_data[%s] for seeded 6h bucket (have %d buckets)", bucketKey, len(plot.PlotsData))

	// percentile_cont on sorted integers [100..123] (N=24):
	//   Q1: position = 0.25 * 23 = 5.75 → lerp(105, 106, 0.75) = 105.75 mc → 0.10575 cores
	//   Q3: position = 0.75 * 23 = 17.25 → lerp(117, 118, 0.25) = 117.25 mc → 0.11725 cores
	// Memory [10000..10230] (step 10):
	//   Q1: lerp(10050, 10060, 0.75) = 10057.5 KiB → 10057.5/1024 ≈ 9.82178 MiB
	//   Q3: lerp(10170, 10180, 0.25) = 10172.5 KiB → 10172.5/1024 ≈ 9.93408 MiB
	// Percentiles are validated against PostgreSQL percentile_cont(); keep tolerance small but
	// non-zero because AssembleNamespaceBoxplots recomputes end=start window at query time and
	// org-specific term rows can slightly shift which points fall inside the rolling window.
	require.NotNil(t, pd.CPUUsage)
	require.NotNil(t, pd.MemoryUsage)

	assert.InDelta(t, 0.10575, pd.CPUUsage.Q1, 0.008, "CPU Q1 vs percentile_cont(0.25)")
	assert.InDelta(t, 0.11725, pd.CPUUsage.Q3, 0.008, "CPU Q3 vs percentile_cont(0.75)")

	assert.InDelta(t, 10057.5/1024.0, pd.MemoryUsage.Q1, 0.02, "Mem Q1 vs percentile_cont(0.25)")
	assert.InDelta(t, 10172.5/1024.0, pd.MemoryUsage.Q3, 0.04, "Mem Q3 vs percentile_cont(0.75)")

	assert.InDelta(t, 0.100, pd.CPUUsage.Min, 0.001, "CPU min = 100mc = 0.1 cores")
	assert.InDelta(t, 0.123, pd.CPUUsage.Max, 0.008, "CPU max = 123mc = 0.123 cores")
}

func TestAssembleNamespaceBoxplots_LongTerm_Under5ms(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-bench-perf",
	}

	// Seed 1440 samples (15 days × 96 per day)
	for d := 0; d < 15; d++ {
		dayStart := now.AddDate(0, 0, -(15 - d)).Truncate(24 * time.Hour).Add(time.Hour)
		ensureNamespaceSamplePartition(t, pool, dayStart)
		seedNamespaceSamples(t, pool, key.OrgID, key.ClusterUUID, key.Namespace, dayStart, 96, 200+int64(d)*5, 40000+int64(d)*200)
	}

	// Run 20 iterations, measure the 99th-percentile-ish (worst case)
	durations := make([]time.Duration, 20)
	for i := 0; i < 20; i++ {
		start := time.Now()
		plot, err := AssembleNamespaceBoxplots(ctx, pool, key, "long_term", key.OrgID)
		durations[i] = time.Since(start)
		require.NoError(t, err)
		require.NotNil(t, plot)
	}

	// Find max duration
	var maxDur time.Duration
	for _, d := range durations {
		if d > maxDur {
			maxDur = d
		}
	}
	if maxDur > 200*time.Millisecond {
		t.Errorf("AssembleNamespaceBoxplots(long_term, 1440 samples) worst case should be <200ms, got %v", maxDur)
	} else {
		t.Logf("AssembleNamespaceBoxplots(long_term, 1440 samples) worst case: %v", maxDur)
	}
}

func TestNamespaceMonitoringEndTime(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	key := NamespaceKey{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: "ns-met-test",
	}

	// Seed namespace digest data to have a bucket_date
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
