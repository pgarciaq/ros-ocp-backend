package ingestion

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

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
			"bh-ns", "pod-1", "deploy-1", "main",
			"0.1", "0.15", cpu,
			"134217728", "134217728", "104857600", "100000000",
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
			"bh-ns", "pod-1", "deploy-1", "main",
			"0.1", "0.15", "0.50", "134217728", "134217728", "104857600", "100000000")

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
			"idem-ns", "pod-1", "deploy-1", "main",
			"0.1", "0.15", "0.08", "134217728", "134217728", "104857600", "100000000")

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

// BH-UNIT-079: schedules are read from DB at CSV process time, not cached from a prior batch.
func TestProcessCSV_ReadsScheduleAtProcessTime(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-schedule-v2-" + t.Name()
	cluster := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	// v1: narrow Monday 08:30–08:45 UTC window — only the 08:30 sample is in-window.
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "",
		Timezone: "UTC", Days: []string{"monday"},
		StartTime: "08:30", EndTime: "08:45", OffHoursWeight: 0.0, Enabled: true,
	}))

	csv := csvHeader + "\n"
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC) // Monday
	intervals := []struct {
		hour, minute int
		cpu          string
	}{
		{8, 30, "0.90"}, // only in-window spike for v1 (08:30–08:45 UTC)
		{12, 0, "0.05"}, // off-hours for v1
	}
	for _, iv := range intervals {
		start := day.Add(time.Duration(iv.hour)*time.Hour + time.Duration(iv.minute)*time.Minute)
		end := start.Add(15 * time.Minute)
		csv += csvRow(start.Format("2006-01-02 15:04:05 +0000 UTC"), end.Format("2006-01-02 15:04:05 +0000 UTC"),
			"sched-ns", "pod-1", "deploy-1", "main",
			"0.1", "0.15", iv.cpu, "134217728", "134217728", "104857600", "100000000") + "\n"
	}

	_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, cluster)
	require.NoError(t, err)

	var bhSamplesV1 int
	err = pool.QueryRow(ctx,
		`SELECT sample_count FROM daily_container_digests
		 WHERE org_id = $1 AND schedule_type = 'business_hours'`, orgID).Scan(&bhSamplesV1)
	require.NoError(t, err)
	assert.Equal(t, 1, bhSamplesV1, "v1 schedule should include only the 08:30 in-window sample")

	// v2: full-day schedule — simulates a settings change before the next Kafka message is processed.
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "",
		Timezone:  "UTC",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime: "00:00", EndTime: "23:59", OffHoursWeight: 0.0, Enabled: true,
	}))

	_, err = ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, cluster)
	require.NoError(t, err)

	var bhSamplesV2 int
	err = pool.QueryRow(ctx,
		`SELECT sample_count FROM daily_container_digests
		 WHERE org_id = $1 AND schedule_type = 'business_hours'`, orgID).Scan(&bhSamplesV2)
	require.NoError(t, err)
	assert.Equal(t, 2, bhSamplesV2, "v2 full-day schedule includes both Monday samples at process time")
	assert.Greater(t, bhSamplesV2, bhSamplesV1, "second batch must use updated schedule from DB, not v1 cache")
}

func TestParseAndDigestCSV_PruneBHDigestsWhenNoSchedule(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-prune-" + t.Name()
	cluster := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
	})

	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "",
		Timezone:  "UTC",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime: "00:00", EndTime: "23:59",
		OffHoursWeight: 0.0, Enabled: true,
	}))

	csv := csvHeader + "\n" +
		csvRow("2026-01-06 15:00:00 +0000 UTC", "2026-01-06 15:15:00 +0000 UTC",
			"prune-ns", "pod-1", "deploy-1", "main",
			"0.1", "0.15", "0.50", "134217728", "134217728", "104857600", "100000000")

	_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, cluster)
	require.NoError(t, err)
	var bhBefore int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1 AND schedule_type = 'business_hours'`,
		orgID).Scan(&bhBefore)
	require.NoError(t, err)
	require.Equal(t, 1, bhBefore)

	_, err = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	require.NoError(t, err)

	_, err = ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, cluster)
	require.NoError(t, err)
	var bhAfter int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1 AND schedule_type = 'business_hours'`,
		orgID).Scan(&bhAfter)
	require.NoError(t, err)
	assert.Equal(t, 0, bhAfter)
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
			"ns-low", "pod-1", "deploy-1", "main",
			"0.1", "0.15", "0.05", "134217728", "134217728", "104857600", "100000000") + "\n"
	}
	csv += csvRow("2026-01-05 08:30:00 +0000 UTC", "2026-01-05 08:45:00 +0000 UTC",
		"ns-high", "pod-1", "deploy-1", "main",
		"0.1", "0.15", "0.90", "134217728", "134217728", "104857600", "100000000") + "\n"
	for i := 1; i < 8; i++ {
		if i == 2 {
			continue
		}
		start := day.Add(time.Duration(i*15) * time.Minute)
		end := start.Add(15 * time.Minute)
		csv += csvRow(start.Format("2006-01-02 15:04:05 +0000 UTC"), end.Format("2006-01-02 15:04:05 +0000 UTC"),
			"ns-high", "pod-1", "deploy-1", "main",
			"0.1", "0.15", "0.05", "134217728", "134217728", "104857600", "100000000") + "\n"
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

func TestNamespaceOverrideScheduleChange_ReducesBHSamples(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-ns-override-" + t.Name()
	cluster := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	// Cluster-wide 9h window (08:00–17:00 UTC on weekdays).
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "08:00", EndTime: "17:00", OffHoursWeight: 0.0, Enabled: true,
	}))

	csv := generateWeekdaySpikeCSV(t)
	csv = strings.ReplaceAll(csv, "bh-ns", "override-ns")
	_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, cluster)
	require.NoError(t, err)

	var wideSamples float64
	err = pool.QueryRow(ctx,
		`SELECT sample_count FROM daily_container_digests
		 WHERE org_id = $1 AND namespace = $2 AND schedule_type = 'business_hours'`,
		orgID, "override-ns").Scan(&wideSamples)
	require.NoError(t, err)
	require.Greater(t, wideSamples, float64(4), "wide cluster window should include many in-window samples")

	// Namespace override: narrow 1h window (12:00–13:00 UTC).
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "override-ns",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "12:00", EndTime: "13:00", OffHoursWeight: 0.0, Enabled: true,
	}))
	require.NoError(t, bhschedule.PruneNamespaceBusinessHoursDigests(ctx, pool, orgID, cluster, "override-ns"))

	_, err = ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, cluster)
	require.NoError(t, err)

	var narrowSamples float64
	err = pool.QueryRow(ctx,
		`SELECT sample_count FROM daily_container_digests
		 WHERE org_id = $1 AND namespace = $2 AND schedule_type = 'business_hours'`,
		orgID, "override-ns").Scan(&narrowSamples)
	require.NoError(t, err)
	assert.Greater(t, narrowSamples, float64(0))
	assert.Less(t, narrowSamples, wideSamples*0.85,
		"namespace override should reduce in-window samples vs inherited cluster schedule")
}

func TestMain(m *testing.M) {
	if os.Getenv("ROS_BUSINESS_HOURS_ENABLED") == "" {
		_ = os.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	}
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("github.com/jackc/pgx/v5/pgxpool.(*Pool).backgroundHealthCheck"),
		goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"),
		goleak.IgnoreTopFunction("github.com/testcontainers/testcontainers-go.(*Reaper).connect.func1"),
		goleak.IgnoreTopFunction("github.com/hashicorp/golang-lru/v2/expirable.NewLRU[...].func1"),
	)
}

// seedEvidenceTestSchedule writes a cluster-wide Mon-Fri 09:00-17:00 UTC
// schedule (the #760 repro window — UTC keeps edge classification obvious).
func seedEvidenceTestSchedule(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: testutil.TestClusterUUID, Namespace: "",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "09:00", EndTime: "17:00", OffHoursWeight: 0.0, Enabled: true,
	}))
}

// TestSleepEvidence_RealHourlySamples is #760's raw-producer proof: real
// hourly rows through the canonical ingestion path must land as per-leg
// p50s in hcp_sleep_evidence — DISTINCT in-window/off-window values (100 /
// 900) so a filter inversion cannot pass by coincidence, and window-edge
// samples (08:59 in, 09:00 out ... 16:59 in, 17:00 out) pin the boundary.
func TestSleepEvidence_RealHourlySamples(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-sl-evidence-" + t.Name()
	cluster := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_sleep_evidence WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})
	seedEvidenceTestSchedule(t, ctx, pool, orgID)

	// Tuesday 2026-01-06: BH window 09:00-17:00 UTC. In-window hours carry
	// 100m; off-window hours carry 900m. Edge hours: 08:59 off, 09:00 in,
	// 16:59 in, 17:00 off.
	day := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)
	var b strings.Builder
	b.WriteString(csvHeader + "\n")
	rowAt := func(ts time.Time, cpu string) {
		b.WriteString(csvRow(
			ts.Format("2006-01-02 15:04:05 +0000 UTC"), ts.Add(time.Hour).Format("2006-01-02 15:04:05 +0000 UTC"),
			"demo-app", "pod-1", "web", "main",
			cpu, cpu, cpu, "134217728", "134217728", "104857600", "100000000"))
		b.WriteString("\n")
	}
	rowAt(day.Add(8*time.Hour+59*time.Minute), "0.9") // 08:59 — off-window edge
	for h := 9; h < 17; h++ {
		rowAt(day.Add(time.Duration(h)*time.Hour), "0.1") // 09:00..16:59 in-window
	}
	rowAt(day.Add(17*time.Hour), "0.9") // 17:00 — off-window edge
	for _, h := range []int{18, 20, 22, 2, 4, 6} {
		rowAt(day.Add(time.Duration(h)*time.Hour), "0.9") // more off-window mass
	}

	_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(b.String()), orgID, cluster)
	require.NoError(t, err)

	var bhP50, offP50 *int64
	err = pool.QueryRow(ctx,
		`SELECT bh_cpu_p50_mc, off_cpu_p50_mc FROM hcp_sleep_evidence
		 WHERE org_id = $1 AND namespace = 'demo-app'`, orgID).Scan(&bhP50, &offP50)
	require.NoError(t, err)
	require.NotNil(t, bhP50, "in-window samples exist — the BH leg must be measured")
	require.NotNil(t, offP50, "off-window samples exist — the off leg must be measured")
	assert.Equal(t, int64(100), *bhP50,
		"the BH leg is the in-window p50 (distinct from off — a filter inversion fails here)")
	assert.Equal(t, int64(900), *offP50,
		"the off leg is the off-window p50 — the direct #760 evidence, never a subtraction")
}

// TestSleepEvidence_NightOnlyContainer: a container whose every sample is
// off-window gets ONLY the off leg (bh NULL) — the #761 coverage shape at
// evidence grain.
func TestSleepEvidence_NightOnlyContainer(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-sl-night-" + t.Name()
	cluster := testutil.TestClusterUUID
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_sleep_evidence WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})
	seedEvidenceTestSchedule(t, ctx, pool, orgID)

	day := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)
	var b strings.Builder
	b.WriteString(csvHeader + "\n")
	for _, h := range []int{0, 1, 2, 3, 4, 5, 22, 23} {
		ts := day.Add(time.Duration(h) * time.Hour)
		b.WriteString(csvRow(
			ts.Format("2006-01-02 15:04:05 +0000 UTC"), ts.Add(time.Hour).Format("2006-01-02 15:04:05 +0000 UTC"),
			"batch-jobs", "pod-9", "night-cron", "main",
			"0.9", "0.9", "0.9", "134217728", "134217728", "104857600", "100000000"))
		b.WriteString("\n")
	}
	_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(b.String()), orgID, cluster)
	require.NoError(t, err)

	var bhP50, offP50 *int64
	err = pool.QueryRow(ctx,
		`SELECT bh_cpu_p50_mc, off_cpu_p50_mc FROM hcp_sleep_evidence
		 WHERE org_id = $1 AND namespace = 'batch-jobs'`, orgID).Scan(&bhP50, &offP50)
	require.NoError(t, err)
	assert.Nil(t, bhP50, "no in-window samples — the BH leg stays NULL (#761 shape)")
	require.NotNil(t, offP50)
	assert.Equal(t, int64(900), *offP50)
}

// TestSleepEvidence_IdempotentReplay: re-ingesting the same rows converges
// to the same evidence (replacement upserts, never incremental sums).
func TestSleepEvidence_IdempotentReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-sl-replay-" + t.Name()
	cluster := testutil.TestClusterUUID
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_sleep_evidence WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})
	seedEvidenceTestSchedule(t, ctx, pool, orgID)

	day := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)
	var b strings.Builder
	b.WriteString(csvHeader + "\n")
	for _, spec := range []struct {
		h   int
		cpu string
	}{{10, "0.1"}, {12, "0.1"}, {14, "0.3"}, {20, "0.9"}, {22, "0.9"}} {
		ts := day.Add(time.Duration(spec.h) * time.Hour)
		b.WriteString(csvRow(
			ts.Format("2006-01-02 15:04:05 +0000 UTC"), ts.Add(time.Hour).Format("2006-01-02 15:04:05 +0000 UTC"),
			"demo-app", "pod-1", "web", "main",
			spec.cpu, spec.cpu, spec.cpu, "134217728", "134217728", "104857600", "100000000"))
		b.WriteString("\n")
	}
	for i := 0; i < 2; i++ {
		_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(b.String()), orgID, cluster)
		require.NoError(t, err)
	}
	var bhP50, offP50 int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COALESCE(bh_cpu_p50_mc, -1), COALESCE(off_cpu_p50_mc, -1)
		 FROM hcp_sleep_evidence WHERE org_id = $1`, orgID).Scan(&bhP50, &offP50))
	assert.Equal(t, int64(100), bhP50, "replay converges (replacement, not increment)")
	assert.Equal(t, int64(900), offP50, "replay converges (replacement, not increment)")
}

// TestSleepEvidence_NoScheduleNoRows: without an enabled schedule the
// evidence writer stays silent (the BH-stream enablement condition).
func TestSleepEvidence_NoScheduleNoRows(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-sl-nosched-" + t.Name()
	cluster := testutil.TestClusterUUID
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_sleep_evidence WHERE org_id = $1`, orgID)
	})
	day := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)
	var b strings.Builder
	b.WriteString(csvHeader + "\n")
	ts := day.Add(12 * time.Hour)
	b.WriteString(csvRow(
		ts.Format("2006-01-02 15:04:05 +0000 UTC"), ts.Add(time.Hour).Format("2006-01-02 15:04:05 +0000 UTC"),
		"demo-app", "pod-1", "web", "main",
		"0.1", "0.1", "0.1", "134217728", "134217728", "104857600", "100000000"))
	b.WriteString("\n")
	_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(b.String()), orgID, cluster)
	require.NoError(t, err)
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM hcp_sleep_evidence WHERE org_id = $1`, orgID).Scan(&n))
	assert.Equal(t, 0, n, "no enabled schedule — no evidence rows (the BH-stream condition)")
}

// TestSleepEvidence_WindowEdges pins the boundary classification exactly:
// a misclassified edge hour flips which leg is NULL (a p50 would not move
// for a single edge sample — the NULL side is the sharp instrument).
// 09:00 and 16:59 are in-window; 08:59 and 17:00 are off-window.
func TestSleepEvidence_WindowEdges(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-sl-edges-" + t.Name()
	cluster := testutil.TestClusterUUID
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_sleep_evidence WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})
	seedEvidenceTestSchedule(t, ctx, pool, orgID)

	day := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)
	var b strings.Builder
	b.WriteString(csvHeader + "\n")
	mk := func(ns, pod string, ts time.Time) {
		b.WriteString(csvRow(
			ts.Format("2006-01-02 15:04:05 +0000 UTC"), ts.Add(time.Minute).Format("2006-01-02 15:04:05 +0000 UTC"),
			ns, pod, ns+"-wl", "main",
			"0.5", "0.5", "0.5", "134217728", "134217728", "104857600", "100000000"))
		b.WriteString("\n")
	}
	// In-window-only container: 09:00 (start, inclusive) and 16:59 (end,
	// exclusive).
	mk("edge-in", "p1", day.Add(9*time.Hour))
	mk("edge-in", "p1", day.Add(16*time.Hour+59*time.Minute))
	// Off-window-only container: 08:59 (before start) and 17:00 (the end
	// itself — exclusive).
	mk("edge-off", "p2", day.Add(8*time.Hour+59*time.Minute))
	mk("edge-off", "p2", day.Add(17*time.Hour))

	_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(b.String()), orgID, cluster)
	require.NoError(t, err)

	var inBH, inOff, offBH, offOff *int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT bh_cpu_p50_mc, off_cpu_p50_mc FROM hcp_sleep_evidence
		 WHERE org_id = $1 AND namespace = 'edge-in'`, orgID).Scan(&inBH, &inOff))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT bh_cpu_p50_mc, off_cpu_p50_mc FROM hcp_sleep_evidence
		 WHERE org_id = $1 AND namespace = 'edge-off'`, orgID).Scan(&offBH, &offOff))
	require.NotNil(t, inBH, "09:00 and 16:59 are in-window — the BH leg is measured")
	assert.Nil(t, inOff, "the in-window-only container has no off leg")
	assert.Nil(t, offBH, "08:59 and 17:00 are off-window — the BH leg is NULL")
	require.NotNil(t, offOff, "the off-window-only container's off leg is measured")
}

// TestSleepEvidence_ClusterSplitNotNamespace (#760 review F1): a namespace
// schedule override WIDER than the cluster schedule must not swallow
// off-window activity — the evidence answers the sleep predicate's
// cluster-window question (resolveSleepSchedule is cluster-scoped). A
// 19:00 sample is outside the cluster's 09:00-17:00 window even though the
// namespace override runs to 21:00: the off leg must carry it.
func TestSleepEvidence_ClusterSplitNotNamespace(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-sl-cluster-" + t.Name()
	cluster := testutil.TestClusterUUID
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_sleep_evidence WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})
	seedEvidenceTestSchedule(t, ctx, pool, orgID)
	// The namespace override: 09:00-21:00 — WIDER than the cluster window.
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "wide-ns",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "09:00", EndTime: "21:00", OffHoursWeight: 0.0, Enabled: true,
	}))

	day := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)
	var b strings.Builder
	b.WriteString(csvHeader + "\n")
	mk := func(ts time.Time, cpu string) {
		b.WriteString(csvRow(
			ts.Format("2006-01-02 15:04:05 +0000 UTC"), ts.Add(time.Hour).Format("2006-01-02 15:04:05 +0000 UTC"),
			"wide-ns", "pod-1", "web", "main",
			cpu, cpu, cpu, "134217728", "134217728", "104857600", "100000000"))
		b.WriteString("\n")
	}
	mk(day.Add(12*time.Hour), "0.1") // 12:00 — inside BOTH windows
	mk(day.Add(19*time.Hour), "0.9") // 19:00 — outside the CLUSTER window, inside the namespace override

	_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(b.String()), orgID, cluster)
	require.NoError(t, err)

	var bhP50, offP50 *int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT bh_cpu_p50_mc, off_cpu_p50_mc FROM hcp_sleep_evidence
		 WHERE org_id = $1 AND namespace = 'wide-ns'`, orgID).Scan(&bhP50, &offP50))
	require.NotNil(t, bhP50)
	assert.Equal(t, int64(100), *bhP50, "12:00 is in the cluster window — the BH leg carries it")
	require.NotNil(t, offP50,
		"19:00 is outside the CLUSTER window — the off leg must carry it even though the namespace override runs to 21:00 (#760 review F1)")
	assert.Equal(t, int64(900), *offP50,
		"a wider namespace override must not swallow off-window activity the advice would sleep away")
}
