// Package integration holds cross-component business hours tests (settings, ingestion,
// recommendations). Requires PostgreSQL via testcontainers; skipped with -short.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/reship"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

const (
	containerCSVHeader = "interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count"
	namespaceCSVHeader = "interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_limit_namespace_sum,cpu_usage_namespace_avg,cpu_usage_namespace_max,cpu_usage_namespace_min,cpu_throttle_namespace_avg,cpu_throttle_namespace_max,memory_request_namespace_sum,memory_limit_namespace_sum,memory_usage_namespace_avg,memory_usage_namespace_max,memory_usage_namespace_min,memory_rss_usage_namespace_avg,memory_rss_usage_namespace_max"
)

func enableBusinessHoursForTest(t *testing.T) {
	t.Helper()
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	config.ResetForTest()
}

func disableBusinessHoursForTest(t *testing.T) {
	t.Helper()
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "false")
	config.ResetForTest()
}

func validBHPayload() map[string]interface{} {
	return map[string]interface{}{
		"timezone": "America/New_York",
		"schedule": map[string]interface{}{
			"days":       []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
			"start_time": "08:00",
			"end_time":   "17:00",
		},
		"off_hours_weight": 0.0,
		"enabled":          true,
	}
}

type noopReshipTrigger struct{}

func (noopReshipTrigger) TriggerReship(context.Context, string, uuid.UUID) error { return nil }

func setupBHAPI(t *testing.T, pool *pgxpool.Pool, orgID string) *echo.Echo {
	t.Helper()
	enableBusinessHoursForTest(t)
	prev := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = prev })

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("Identity", identity.XRHID{
				Identity: identity.Identity{OrgID: orgID},
			})
			return next(c)
		}
	})
	v1 := e.Group("/api/cost-management/v1")
	api.RegisterBusinessHoursRoutes(v1, api.NewBusinessHoursSettingsHandler(noopReshipTrigger{}))
	return e
}

func serveBH(t *testing.T, e *echo.Echo, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func seedCluster(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string) {
	t.Helper()
	ctx := context.Background()
	var tenantID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO rh_accounts (org_id) VALUES ($1)
		ON CONFLICT (org_id) DO UPDATE SET org_id = EXCLUDED.org_id
		RETURNING id`, orgID).Scan(&tenantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2::uuid, 'bh-int-cluster', 'src-bh-int', now()) ON CONFLICT DO NOTHING`,
		tenantID, clusterUUID)
	require.NoError(t, err)
}

func cleanupOrgBHData(t *testing.T, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
	_, _ = pool.Exec(ctx, `DELETE FROM daily_namespace_digests WHERE org_id = $1`, orgID)
	_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
}

func containerCSVRow(start, end, ns, pod, wl, wlType, cn, cpuUsage string) string {
	return fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,0.1,0.15,%s,0.001,134217728,134217728,104857600,100000000,0",
		start, end, ns, pod, wl, wlType, cn, cpuUsage)
}

// spikeDay is a Tuesday aligned with testutil.BaseDate for predictable weekday BH windows.
func spikeDay() time.Time {
	d := testutil.BaseDate
	for d.Weekday() != time.Tuesday {
		d = d.AddDate(0, 0, 1)
		if d.Sub(testutil.BaseDate) > 7*24*time.Hour {
			return testutil.BaseDate
		}
	}
	return d
}

// generateWeekdaySpikeCSV produces 96×15m samples on spikeDay: low CPU in America/New_York
// business hours (08:00–17:00 EST → 13:00–22:00 UTC), high CPU off-hours.
func generateWeekdaySpikeCSV(t *testing.T) string {
	t.Helper()
	day := spikeDay()
	var b strings.Builder
	b.WriteString(containerCSVHeader + "\n")
	for i := 0; i < 96; i++ {
		start := day.Add(time.Duration(i*15) * time.Minute)
		end := start.Add(15 * time.Minute)
		cpu := "0.95"
		if i >= 52 && i < 88 {
			cpu = "0.05"
		}
		b.WriteString(containerCSVRow(
			start.Format("2006-01-02 15:04:05 +0000 UTC"),
			end.Format("2006-01-02 15:04:05 +0000 UTC"),
			"bh-int-ns", "pod-1", "deploy-1", "deployment", "main", cpu,
		))
		b.WriteString("\n")
	}
	return b.String()
}

func putOrgBusinessHours(t *testing.T, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	seedCluster(t, pool, orgID, testutil.TestClusterUUID)
	e := setupBHAPI(t, pool, orgID)
	rec := serveBH(t, e, http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/business-hours",
		validBHPayload())
	require.Equal(t, http.StatusAccepted, rec.Code)
}

func ingestContainerCSV(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID, csv string) {
	t.Helper()
	ctx := context.Background()
	_, err := ingestion.ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, clusterUUID)
	require.NoError(t, err)
}

func countDigestsBySchedule(t *testing.T, pool *pgxpool.Pool, orgID, scheduleType string) int {
	t.Helper()
	ctx := context.Background()
	var n int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1 AND schedule_type = $2::digest_schedule_type`,
		orgID, scheduleType).Scan(&n)
	require.NoError(t, err)
	return n
}

func recommendAndEnrichContainer(
	t *testing.T,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
) []model.NativeContainerResult {
	t.Helper()
	ctx := context.Background()
	start := spikeDay()
	end := start
	recs, err := engine.RecommendAllWorkloads(ctx, pool, orgID, clusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	native := buildNativeFromRecs(recs)
	require.NoError(t, engine.EnrichNativeContainerResultsWithBusinessHours(ctx, pool, orgID, native))
	return native
}

func buildNativeFromRecs(recs []engine.ContainerRec) []model.NativeContainerResult {
	if len(recs) == 0 {
		return nil
	}
	first := recs[0]
	result := model.NativeContainerResult{
		ID:              model.NativeContainerID(first.ClusterUUID, first.Namespace, first.Workload, first.WorkloadType, first.ContainerName),
		ClusterUUID:     first.ClusterUUID,
		Container:       first.ContainerName,
		Project:         first.Namespace,
		Workload:        first.Workload,
		WorkloadType:    first.WorkloadType,
		Recommendations: make(map[string]model.TermRecommendation),
	}
	for _, r := range recs {
		termKey := r.Term + "_term"
		term, ok := result.Recommendations[termKey]
		if !ok {
			term = model.TermRecommendation{}
		}
		cpu := r.RecCPURequestMC
		mem := r.RecMemRequestKiB
		eng := &model.EngineRecommendation{
			CPURequestMillicores: &cpu,
			MemRequestKiB:        &mem,
		}
		switch r.Engine {
		case "cost":
			term.Cost = eng
		case "performance":
			term.Performance = eng
		}
		result.Recommendations[termKey] = term
	}
	return []model.NativeContainerResult{result}
}

func shortTermCostCPU(native []model.NativeContainerResult) (allHours int64, bh *int64) {
	term := native[0].Recommendations["short_term"]
	if term.Cost == nil || term.Cost.CPURequestMillicores == nil {
		return 0, nil
	}
	allHours = *term.Cost.CPURequestMillicores
	if term.Cost.BusinessHours != nil && term.Cost.BusinessHours.CPURequestMillicores != nil {
		v := *term.Cost.BusinessHours.CPURequestMillicores
		return allHours, &v
	}
	return allHours, nil
}

// BH-INT-030: settings PUT → ingest → dual digest streams → dual recommendations.
func TestFullPipeline_SettingsToDualRecommendation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-int-030"
	cleanupOrgBHData(t, pool, orgID)
	t.Cleanup(func() { cleanupOrgBHData(t, pool, orgID) })

	putOrgBusinessHours(t, pool, orgID)
	ingestContainerCSV(t, pool, orgID, testutil.TestClusterUUID, generateWeekdaySpikeCSV(t))

	assert.Equal(t, 1, countDigestsBySchedule(t, pool, orgID, "all_hours"))
	assert.Equal(t, 1, countDigestsBySchedule(t, pool, orgID, "business_hours"))

	native := recommendAndEnrichContainer(t, pool, orgID, testutil.TestClusterUUID)
	allCPU, bhCPU := shortTermCostCPU(native)
	require.NotNil(t, bhCPU)
	assert.True(t, allCPU > 50, "all-hours should reflect high off-hours usage")
	assert.True(t, *bhCPU < allCPU, "business hours CPU should be lower than all-hours on spike fixture")
}

// BH-INT-031: DELETE schedule → re-ingest removes business_hours digests.
func TestDeleteSchedule_RemovesBHDigests(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-int-031"
	cleanupOrgBHData(t, pool, orgID)
	t.Cleanup(func() { cleanupOrgBHData(t, pool, orgID) })

	putOrgBusinessHours(t, pool, orgID)
	csv := generateWeekdaySpikeCSV(t)
	ingestContainerCSV(t, pool, orgID, testutil.TestClusterUUID, csv)
	require.Equal(t, 1, countDigestsBySchedule(t, pool, orgID, "business_hours"))

	e := setupBHAPI(t, pool, orgID)
	rec := serveBH(t, e, http.MethodDelete,
		"/api/cost-management/v1/recommendations/openshift/settings/business-hours", nil)
	require.Equal(t, http.StatusNoContent, rec.Code)

	ingestContainerCSV(t, pool, orgID, testutil.TestClusterUUID, csv)
	assert.Equal(t, 1, countDigestsBySchedule(t, pool, orgID, "all_hours"))
	assert.Equal(t, 0, countDigestsBySchedule(t, pool, orgID, "business_hours"))
}

// BH-INT-032: org-level PUT applies BH to all clusters in the org.
func TestMultiCluster_OrgPUT_BothClustersGetBH(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-int-032"
	clusterA := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1"
	clusterB := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2"
	cleanupOrgBHData(t, pool, orgID)
	t.Cleanup(func() { cleanupOrgBHData(t, pool, orgID) })

	seedCluster(t, pool, orgID, clusterA)
	seedCluster(t, pool, orgID, clusterB)
	e := setupBHAPI(t, pool, orgID)
	rec := serveBH(t, e, http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/business-hours",
		validBHPayload())
	require.Equal(t, http.StatusAccepted, rec.Code)

	csv := generateWeekdaySpikeCSV(t)
	ingestContainerCSV(t, pool, orgID, clusterA, csv)
	ingestContainerCSV(t, pool, orgID, clusterB, csv)

	for _, cluster := range []string{clusterA, clusterB} {
		ctx := context.Background()
		var bhCount int
		err := pool.QueryRow(ctx,
			`SELECT count(*) FROM daily_container_digests
			 WHERE org_id = $1 AND cluster_uuid = $2::uuid AND schedule_type = 'business_hours'`,
			orgID, cluster).Scan(&bhCount)
		require.NoError(t, err)
		assert.Equal(t, 1, bhCount, "cluster %s should have business_hours digest", cluster)

		native := recommendAndEnrichContainer(t, pool, orgID, cluster)
		_, bhCPU := shortTermCostCPU(native)
		require.NotNil(t, bhCPU, "cluster %s should have business_hours recommendation", cluster)
	}
}

// BH-INT-034: org A schedule must not affect org B recommendations.
func TestCrossTenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	orgA := "org-bh-int-034a"
	orgB := "org-bh-int-034b"
	cleanupOrgBHData(t, pool, orgA)
	cleanupOrgBHData(t, pool, orgB)
	t.Cleanup(func() {
		cleanupOrgBHData(t, pool, orgA)
		cleanupOrgBHData(t, pool, orgB)
	})

	putOrgBusinessHours(t, pool, orgA)
	seedCluster(t, pool, orgB, testutil.TestClusterUUID)
	csv := generateWeekdaySpikeCSV(t)
	ingestContainerCSV(t, pool, orgA, testutil.TestClusterUUID, csv)
	ingestContainerCSV(t, pool, orgB, testutil.TestClusterUUID, csv)

	nativeA := recommendAndEnrichContainer(t, pool, orgA, testutil.TestClusterUUID)
	_, bhA := shortTermCostCPU(nativeA)
	require.NotNil(t, bhA)

	nativeB := recommendAndEnrichContainer(t, pool, orgB, testutil.TestClusterUUID)
	detail := model.BuildDetailResponse(&nativeB[0], nil, time.Time{})
	b, err := json.Marshal(detail)
	require.NoError(t, err)
	assert.NotContains(t, string(b), `"business_hours"`)
}

// BH-INT-026: namespace recommendations include business_hours when configured.
func TestNamespacePlugin_DualStream(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-int-026"
	cleanupOrgBHData(t, pool, orgID)
	t.Cleanup(func() { cleanupOrgBHData(t, pool, orgID) })

	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: testutil.TestClusterUUID, Namespace: "",
		Timezone:  "UTC",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime: "00:00", EndTime: "23:59", OffHoursWeight: 0.0, Enabled: true,
	}))

	var nsCSV strings.Builder
	nsCSV.WriteString(namespaceCSVHeader + "\n")
	for i := 0; i < 7; i++ {
		day := testutil.BaseDate.AddDate(0, 0, i)
		start := day
		end := start.Add(15 * time.Minute)
		fmt.Fprintf(&nsCSV, "%s,%s,ns-dual,100,200,80,90,70,0,0,1000,2000,800,900,700,600,650\n",
			start.Format("2006-01-02 15:04:05 +0000 UTC"),
			end.Format("2006-01-02 15:04:05 +0000 UTC"))
	}

	require.NoError(t, ingestion.ProcessNamespaceCSVToDigests(ctx, pool, strings.NewReader(nsCSV.String()), orgID, testutil.TestClusterUUID))

	var allCount, bhCount int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_namespace_digests
		 WHERE org_id = $1 AND namespace = $2 AND schedule_type = 'all_hours'`,
		orgID, "ns-dual").Scan(&allCount)
	require.NoError(t, err)
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_namespace_digests
		 WHERE org_id = $1 AND namespace = $2 AND schedule_type = 'business_hours'`,
		orgID, "ns-dual").Scan(&bhCount)
	require.NoError(t, err)
	assert.Equal(t, 7, allCount)
	assert.Equal(t, 7, bhCount)

	start, end := testutil.BaseDate, testutil.BaseDate.AddDate(0, 0, 6)
	nsRecs, err := engine.RecommendAllNamespaces(ctx, pool, orgID, testutil.TestClusterUUID, start, end)
	require.NoError(t, err)
	require.NotEmpty(t, nsRecs)

	var allMediumCPU int64
	for _, r := range nsRecs {
		if r.Term == "medium" && r.Engine == "cost" {
			allMediumCPU = r.RecCPURequestMC
		}
	}
	require.True(t, allMediumCPU > 0)

	allCopy := allMediumCPU
	native := []model.NativeNamespaceResult{{
		ID:          model.NativeNamespaceID(testutil.TestClusterUUID, "ns-dual"),
		ClusterUUID: testutil.TestClusterUUID,
		Project:     "ns-dual",
		Recommendations: map[string]any{
			"medium_term": model.TermRecommendation{
				Cost: &model.EngineRecommendation{CPURequestMillicores: &allCopy},
			},
		},
	}}
	require.NoError(t, engine.EnrichNativeNamespaceResultsWithBusinessHours(ctx, pool, orgID, native))
	term := native[0].Recommendations["medium_term"].(model.TermRecommendation)
	require.NotNil(t, term.Cost.BusinessHours)
	require.NotNil(t, term.Cost.BusinessHours.CPURequestMillicores)
}

// BH-INT-035: schedule change between ingests updates business_hours digests.
func TestScheduleChange_RecomputesDigests(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-int-035"
	cluster := testutil.TestClusterUUID
	cleanupOrgBHData(t, pool, orgID)
	t.Cleanup(func() { cleanupOrgBHData(t, pool, orgID) })

	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "",
		Timezone: "UTC", Days: []string{"monday"},
		StartTime: "08:30", EndTime: "08:45", OffHoursWeight: 0.0, Enabled: true,
	}))

	day := spikeDay()
	for day.Weekday() != time.Monday {
		day = day.AddDate(0, 0, -1)
	}
	csv := containerCSVHeader + "\n"
	intervals := []struct {
		hour, minute int
		cpu          string
	}{
		{8, 30, "0.90"},
		{12, 0, "0.05"},
	}
	for _, iv := range intervals {
		start := day.Add(time.Duration(iv.hour)*time.Hour + time.Duration(iv.minute)*time.Minute)
		end := start.Add(15 * time.Minute)
		csv += containerCSVRow(
			start.Format("2006-01-02 15:04:05 +0000 UTC"), end.Format("2006-01-02 15:04:05 +0000 UTC"),
			"sched-ns", "pod-1", "deploy-1", "deployment", "main", iv.cpu) + "\n"
	}

	ingestContainerCSV(t, pool, orgID, cluster, csv)
	var samplesV1 int
	err := pool.QueryRow(ctx,
		`SELECT sample_count FROM daily_container_digests
		 WHERE org_id = $1 AND schedule_type = 'business_hours'`, orgID).Scan(&samplesV1)
	require.NoError(t, err)
	assert.Equal(t, 1, samplesV1)

	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "",
		Timezone:  "UTC",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime: "00:00", EndTime: "23:59", OffHoursWeight: 0.0, Enabled: true,
	}))

	ingestContainerCSV(t, pool, orgID, cluster, csv)
	var samplesV2 int
	err = pool.QueryRow(ctx,
		`SELECT sample_count FROM daily_container_digests
		 WHERE org_id = $1 AND schedule_type = 'business_hours'`, orgID).Scan(&samplesV2)
	require.NoError(t, err)
	assert.Equal(t, 2, samplesV2)
	assert.Greater(t, samplesV2, samplesV1)
}

// BH-INT-036: kill-switch disables BH digest generation and API enrichment.
func TestKillSwitch_NoBHDigests(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	disableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-int-036"
	cleanupOrgBHData(t, pool, orgID)
	t.Cleanup(func() { cleanupOrgBHData(t, pool, orgID) })

	require.NoError(t, engine.UpsertBusinessHoursSchedule(ctx, pool, engine.BusinessHoursSchedule{
		OrgID: orgID, ClusterUUID: engine.OrgClusterSentinelUUID, Namespace: "",
		Timezone: "America/New_York", Days: []string{"monday"}, StartTime: "08:00", EndTime: "17:00", Enabled: true,
	}))
	seedCluster(t, pool, orgID, testutil.TestClusterUUID)

	ingestContainerCSV(t, pool, orgID, testutil.TestClusterUUID, generateWeekdaySpikeCSV(t))
	assert.Equal(t, 1, countDigestsBySchedule(t, pool, orgID, "all_hours"))
	assert.Equal(t, 0, countDigestsBySchedule(t, pool, orgID, "business_hours"))

	start := spikeDay()
	end := start
	recs, err := engine.RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	native := buildNativeFromRecs(recs)
	require.NoError(t, engine.EnrichNativeContainerResultsWithBusinessHours(ctx, pool, orgID, native))
	detail := model.BuildDetailResponse(&native[0], nil, time.Time{})
	b, err := json.Marshal(detail)
	require.NoError(t, err)
	assert.NotContains(t, string(b), `"business_hours"`)
}

// BH-INT-039: schedule rows are scoped per org_id.
func TestCrossTenant_ScheduleIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgA := "org-bh-int-039a"
	orgB := "org-bh-int-039b"
	cleanupOrgBHData(t, pool, orgA)
	cleanupOrgBHData(t, pool, orgB)
	t.Cleanup(func() {
		cleanupOrgBHData(t, pool, orgA)
		cleanupOrgBHData(t, pool, orgB)
	})

	require.NoError(t, engine.UpsertBusinessHoursSchedule(ctx, pool, engine.BusinessHoursSchedule{
		OrgID: orgA, ClusterUUID: engine.OrgClusterSentinelUUID, Namespace: "",
		Timezone: "America/Chicago", Days: []string{"monday"}, StartTime: "09:00", EndTime: "18:00", Enabled: true,
	}))

	cacheB, err := engine.LoadSchedules(ctx, pool, orgB, testutil.TestClusterUUID)
	require.NoError(t, err)
	resolved := cacheB.Resolve("any-ns")
	assert.False(t, resolved.Enabled)
	assert.Empty(t, resolved.Timezone)
}

// Verify reship trigger is wired on PUT (mock masu); complements full-pipeline tests above.
func TestFullPipeline_PUT_TriggersReshipContract(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForTest(t)
	var captured bool
	masu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "reship_ros") {
			captured = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer masu.Close()
	t.Setenv("KOKU_MASU_URL", masu.URL)

	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-int-reship"
	cleanupOrgBHData(t, pool, orgID)
	t.Cleanup(func() { cleanupOrgBHData(t, pool, orgID) })
	seedCluster(t, pool, orgID, testutil.TestClusterUUID)

	trigger := reship.NewService(pool, reship.ServiceConfig{MasuURL: config.GetConfig().KokuMasuURL})
	require.NotNil(t, trigger)
	prev := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = prev })
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("Identity", identity.XRHID{Identity: identity.Identity{OrgID: orgID}})
			return next(c)
		}
	})
	v1 := e.Group("/api/cost-management/v1")
	api.RegisterBusinessHoursRoutes(v1, api.NewBusinessHoursSettingsHandler(trigger))
	rec := serveBH(t, e, http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/business-hours",
		validBHPayload())
	require.Equal(t, http.StatusAccepted, rec.Code)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !captured {
		time.Sleep(20 * time.Millisecond)
	}
	assert.True(t, captured, "PUT should trigger masu reship_ros POST")
}
