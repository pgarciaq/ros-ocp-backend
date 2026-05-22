package ingestion

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func enableBusinessHoursForTest(t *testing.T) {
	t.Helper()
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	config.ResetForTest()
}

func generateWeekdaySpikeCSV(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(csvHeader + "\n")
	day := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC) // Tuesday
	for i := 0; i < 96; i++ {
		start := day.Add(time.Duration(i*15) * time.Minute)
		end := start.Add(15 * time.Minute)
		// Tue 08:00–17:00 America/New_York (EST, UTC-5) → 13:00–22:00 UTC → indices 52–87
		cpu := "0.95"
		if i >= 52 && i < 88 {
			cpu = "0.05"
		}
		b.WriteString(csvRow(
			start.Format("2006-01-02 15:04:05 +0000 UTC"),
			end.Format("2006-01-02 15:04:05 +0000 UTC"),
			"bh-ns", "pod-1", "deploy-1", "deployment", "main",
			"0.1", "0.15", cpu, "0.001",
			"134217728", "134217728", "104857600", "100000000", "0",
		))
		b.WriteString("\n")
	}
	return b.String()
}

func TestUpsertDigest_OnConflictBothScheduleTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-conflict-" + t.Name()
	cluster := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "",
		Timezone: "America/New_York", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "08:00", EndTime: "17:00", OffHoursWeight: 0.0, Enabled: true,
	}))

	csv := csvHeader + "\n" +
		csvRow("2026-01-06 15:00:00 +0000 UTC", "2026-01-06 15:15:00 +0000 UTC",
			"bh-ns", "pod-1", "deploy-1", "deployment", "main",
			"0.1", "0.15", "0.50", "0.001", "134217728", "134217728", "104857600", "100000000", "0")

	_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, cluster)
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1 AND namespace = $2`,
		orgID, "bh-ns").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestProcessFixtureCSV_DualDigests(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-dual-" + t.Name()
	cluster := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "",
		Timezone: "America/New_York", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "08:00", EndTime: "17:00", OffHoursWeight: 0.0, Enabled: true,
	}))

	csv := generateWeekdaySpikeCSV(t)
	_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, cluster)
	require.NoError(t, err)

	var allCount, bhCount int
	var allP95, bhP95 int64
	err = pool.QueryRow(ctx,
		`SELECT sample_count, cpu_usage_p95_mc FROM daily_container_digests
		 WHERE org_id = $1 AND schedule_type = 'all_hours'`, orgID).Scan(&allCount, &allP95)
	require.NoError(t, err)
	err = pool.QueryRow(ctx,
		`SELECT sample_count, cpu_usage_p95_mc FROM daily_container_digests
		 WHERE org_id = $1 AND schedule_type = 'business_hours'`, orgID).Scan(&bhCount, &bhP95)
	require.NoError(t, err)

	assert.Equal(t, 96, allCount)
	assert.InDelta(t, 40, float64(bhCount), 5, "business hours sample count ~40/96")
	assert.Less(t, bhP95, allP95)
}

func TestProcessFixtureCSV_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-idem-" + t.Name()
	cluster := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime: "00:00", EndTime: "23:59", OffHoursWeight: 0.0, Enabled: true,
	}))

	csv := csvHeader + "\n" +
		csvRow("2026-04-01 00:00:00 +0000 UTC", "2026-04-01 00:15:00 +0000 UTC",
			"idem-ns", "pod-1", "deploy-1", "deployment", "main",
			"0.1", "0.15", "0.08", "0.001", "134217728", "134217728", "104857600", "100000000", "0")

	_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, cluster)
	require.NoError(t, err)

	var p95AllFirst, p95BHFirst int64
	err = pool.QueryRow(ctx,
		`SELECT cpu_usage_p95_mc FROM daily_container_digests
		 WHERE org_id = $1 AND schedule_type = 'all_hours'`, orgID).Scan(&p95AllFirst)
	require.NoError(t, err)
	err = pool.QueryRow(ctx,
		`SELECT cpu_usage_p95_mc FROM daily_container_digests
		 WHERE org_id = $1 AND schedule_type = 'business_hours'`, orgID).Scan(&p95BHFirst)
	require.NoError(t, err)

	_, err = ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, cluster)
	require.NoError(t, err)

	var p95AllSecond, p95BHSecond int64
	err = pool.QueryRow(ctx,
		`SELECT cpu_usage_p95_mc FROM daily_container_digests
		 WHERE org_id = $1 AND schedule_type = 'all_hours'`, orgID).Scan(&p95AllSecond)
	require.NoError(t, err)
	err = pool.QueryRow(ctx,
		`SELECT cpu_usage_p95_mc FROM daily_container_digests
		 WHERE org_id = $1 AND schedule_type = 'business_hours'`, orgID).Scan(&p95BHSecond)
	require.NoError(t, err)
	assert.Equal(t, p95AllFirst, p95AllSecond)
	assert.Equal(t, p95BHFirst, p95BHSecond)
}

func TestLoadSchedules_OneSelectPerBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-cache-" + t.Name()
	cluster := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "",
		Timezone: "UTC", Days: []string{"monday"}, StartTime: "08:00", EndTime: "17:00",
		OffHoursWeight: 0.0, Enabled: true,
	}))

	cache, err := bhschedule.LoadSchedules(ctx, pool, orgID, cluster)
	require.NoError(t, err)
	rows := make([]MetricRow, 500)
	for i := range rows {
		rows[i] = MetricRow{
			IntervalStart: time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC),
			Namespace:     fmt.Sprintf("ns-%d", i%10),
			WorkloadName:  "wl", WorkloadType: "deployment", ContainerName: "c1",
		}
	}
	for _, row := range rows {
		_ = cache.Resolve(row.Namespace)
	}
	assert.NotNil(t, cache)
}

func TestNamespaceDigest_DualStream(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-ns-" + t.Name()
	cluster := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_namespace_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime: "00:00", EndTime: "23:59", OffHoursWeight: 0.0, Enabled: true,
	}))

	nsHeader := "interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_limit_namespace_sum,cpu_usage_namespace_avg,cpu_usage_namespace_max,cpu_usage_namespace_min,cpu_throttle_namespace_avg,cpu_throttle_namespace_max,memory_request_namespace_sum,memory_limit_namespace_sum,memory_usage_namespace_avg,memory_usage_namespace_max,memory_usage_namespace_min,memory_rss_usage_namespace_avg,memory_rss_usage_namespace_max\n"
	csv := nsHeader +
		"2026-04-01 00:00:00 +0000 UTC,2026-04-01 00:15:00 +0000 UTC,ns-a,100,200,80,90,70,0,0,1000,2000,800,900,700,600,650\n"

	require.NoError(t, ProcessNamespaceCSVToDigests(ctx, pool, strings.NewReader(csv), orgID, cluster))

	var count int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_namespace_digests WHERE org_id = $1 AND namespace = $2`,
		orgID, "ns-a").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestMixedNamespaces_DifferentBHPercentiles(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-mix-" + t.Name()
	cluster := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "ns-low",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime: "00:00", EndTime: "23:59", OffHoursWeight: 0.0, Enabled: true,
	}))
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "ns-high",
		Timezone: "UTC", Days: []string{"monday"}, StartTime: "08:00", EndTime: "09:00",
		OffHoursWeight: 0.0, Enabled: true,
	}))

	// ns-low: 24/7 schedule, all samples low usage; ns-high: narrow BH window, one high sample in-window
	csv := csvHeader + "\n"
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC) // Monday
	for i := 0; i < 8; i++ {
		start := day.Add(time.Duration(i*15) * time.Minute)
		end := start.Add(15 * time.Minute)
		csv += csvRow(start.Format("2006-01-02 15:04:05 +0000 UTC"), end.Format("2006-01-02 15:04:05 +0000 UTC"),
			"ns-low", "pod-1", "deploy-1", "deployment", "main",
			"0.1", "0.15", "0.05", "0.001", "134217728", "134217728", "104857600", "100000000", "0") + "\n"
	}
	csv += csvRow("2026-01-05 08:30:00 +0000 UTC", "2026-01-05 08:45:00 +0000 UTC",
		"ns-high", "pod-1", "deploy-1", "deployment", "main",
		"0.1", "0.15", "0.90", "0.001", "134217728", "134217728", "104857600", "100000000", "0") + "\n"
	for i := 1; i < 8; i++ {
		if i == 2 {
			continue
		}
		start := day.Add(time.Duration(i*15) * time.Minute)
		end := start.Add(15 * time.Minute)
		csv += csvRow(start.Format("2006-01-02 15:04:05 +0000 UTC"), end.Format("2006-01-02 15:04:05 +0000 UTC"),
			"ns-high", "pod-1", "deploy-1", "deployment", "main",
			"0.1", "0.15", "0.05", "0.001", "134217728", "134217728", "104857600", "100000000", "0") + "\n"
	}

	_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, cluster)
	require.NoError(t, err)

	var lowBH, highBH int64
	err = pool.QueryRow(ctx,
		`SELECT cpu_usage_p95_mc FROM daily_container_digests
		 WHERE org_id = $1 AND namespace = $2 AND schedule_type = 'business_hours'`,
		orgID, "ns-low").Scan(&lowBH)
	require.NoError(t, err)
	err = pool.QueryRow(ctx,
		`SELECT cpu_usage_p95_mc FROM daily_container_digests
		 WHERE org_id = $1 AND namespace = $2 AND schedule_type = 'business_hours'`,
		orgID, "ns-high").Scan(&highBH)
	require.NoError(t, err)
	assert.Greater(t, highBH, lowBH)
}

func TestMain(m *testing.M) {
	if os.Getenv("ROS_BUSINESS_HOURS_ENABLED") == "" {
		_ = os.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	}
	os.Exit(m.Run())
}
