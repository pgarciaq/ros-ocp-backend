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

	plot, err := AssembleBoxplots(ctx, pool, key, "short_term")
	require.NoError(t, err)
	require.NotNil(t, plot)

	assert.Equal(t, 4, plot.DataPoints, "short_term should have 4 buckets (6h each)")
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

	plot, err := AssembleBoxplots(ctx, pool, key, "medium_term")
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

	plot, err := AssembleBoxplots(ctx, pool, key, "short_term")
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

	plot, err := AssembleBoxplots(ctx, pool, key, "short_term")
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
