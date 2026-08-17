package ingestion_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

const nodeCSVHeader = "interval_start,interval_end,namespace,pod,workload,workload_type,container_name,node," +
	"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
	"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count"

const containerCSVHeader = "interval_start,interval_end,namespace,pod,workload,workload_type,container_name," +
	"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
	"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count"

func enableBusinessHoursForRegressionTest(t *testing.T) {
	t.Helper()
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	config.ResetForTest()
}

func csvRow(start, end, ns, pod, wl, wlType, cn, cpuReq, cpuLimit, cpuUsage, cpuThrottle, memReq, memLimit, memUsage, memRSS, oom string) string {
	return start + "," + end + "," + ns + "," + pod + "," + wl + "," + wlType + "," + cn + "," +
		cpuReq + "," + cpuLimit + "," + cpuUsage + "," + cpuThrottle + "," +
		memReq + "," + memLimit + "," + memUsage + "," + memRSS + "," + oom
}

func buildWeekdaySpikeCSV() string {
	var b strings.Builder
	b.WriteString(containerCSVHeader + "\n")
	day := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC) // Tuesday
	for i := 0; i < 96; i++ {
		start := day.Add(time.Duration(i*15) * time.Minute)
		end := start.Add(15 * time.Minute)
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

func regressionOrgID(t *testing.T, suffix string) string {
	t.Helper()
	return fmt.Sprintf("org-pipeline-%s", suffix)
}

func seedRegressionAccount(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string) {
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
		VALUES ($1, $2::uuid, 'pipeline-test', 'src-pipeline', now()) ON CONFLICT DO NOTHING`,
		tenantID, clusterUUID)
	require.NoError(t, err)
}

func buildSpikeUsageNodeCSV(days int) string {
	var sb strings.Builder
	sb.WriteString(nodeCSVHeader)
	sb.WriteByte('\n')
	start := testutil.RecentStart()
	intervalIdx := 0
	for d := 0; d < days; d++ {
		day := start.AddDate(0, 0, d)
		for h := 0; h < 24; h++ {
			for q := 0; q < 4; q++ {
				cpuUsage := "0.05"
				if intervalIdx%10 == 0 {
					cpuUsage = "0.95"
				}
				intervalIdx++
				intervalStart := time.Date(day.Year(), day.Month(), day.Day(), h, q*15, 0, 0, time.UTC)
				intervalEnd := intervalStart.Add(15 * time.Minute)
				sb.WriteString(fmt.Sprintf(
					"%s,%s,pipeline-ns,pod-1,pipeline-deploy,deployment,main,worker-1,"+
						"8.0,8.0,%s,0.001,33554432.0,33554432.0,1048576.0,524288.0,0\n",
					intervalStart.Format("2006-01-02 15:04:05 +0000 UTC"),
					intervalEnd.Format("2006-01-02 15:04:05 +0000 UTC"),
					cpuUsage,
				))
			}
		}
	}
	return sb.String()
}

func buildNodeContainerCSV(days int, cpuUsage string) string {
	var sb strings.Builder
	sb.WriteString(nodeCSVHeader)
	sb.WriteByte('\n')
	start := testutil.RecentStart()
	for d := 0; d < days; d++ {
		day := start.AddDate(0, 0, d)
		for h := 0; h < 24; h++ {
			for q := 0; q < 4; q++ {
				intervalStart := time.Date(day.Year(), day.Month(), day.Day(), h, q*15, 0, 0, time.UTC)
				intervalEnd := intervalStart.Add(15 * time.Minute)
				sb.WriteString(fmt.Sprintf(
					"%s,%s,pipeline-ns,pod-1,pipeline-deploy,deployment,main,worker-1,"+
						"8.0,8.0,%s,0.001,33554432.0,33554432.0,1048576.0,524288.0,0\n",
					intervalStart.Format("2006-01-02 15:04:05 +0000 UTC"),
					intervalEnd.Format("2006-01-02 15:04:05 +0000 UTC"),
					cpuUsage,
				))
			}
		}
	}
	return sb.String()
}

func buildOversizedStorageCSV(days int) string {
	var sb strings.Builder
	sb.WriteString(`report_period_start,report_period_end,interval_start,interval_end,namespace,pod,node,persistentvolumeclaim,persistentvolume,storageclass,csi_driver,csi_volume_handle,persistentvolumeclaim_capacity_bytes,persistentvolumeclaim_capacity_byte_seconds,volume_request_storage_byte_seconds,persistentvolumeclaim_usage_byte_seconds,persistentvolume_labels,persistentvolumeclaim_labels` + "\n")
	capacity := int64(100 << 30)            // 100 GiB
	usagePerHour := int64(3600 * (1 << 30)) // ~1 GiB average usage
	start := testutil.RecentStart()
	for d := 0; d < days; d++ {
		day := start.AddDate(0, 0, d)
		for h := 0; h < 24; h++ {
			intervalStart := time.Date(day.Year(), day.Month(), day.Day(), h, 0, 0, 0, time.UTC)
			intervalEnd := intervalStart.Add(time.Hour)
			sb.WriteString(fmt.Sprintf(
				"%s,%s,%s,%s,pipeline-ns,pod-1,worker-1,data-pvc,pv-data,gp3,,,%d,%d,%d,%d,,\n",
				day.Format("2006-01-02 15:04:05+00:00"),
				day.AddDate(0, 1, 0).Format("2006-01-02 15:04:05+00:00"),
				intervalStart.Format("2006-01-02 15:04:05+00:00"),
				intervalEnd.Format("2006-01-02 15:04:05+00:00"),
				capacity, capacity*3600, capacity*3600, usagePerHour,
			))
		}
	}
	return sb.String()
}

func mockCostDataForSavings() *costdata.ClusterCostData {
	return &costdata.ClusterCostData{
		DistributionType: "cpu",
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour":      {Infrastructure: 0, Supplementary: 0.01},
			"memory_gb_usage_per_hour":     {Infrastructure: 0, Supplementary: 0.02},
			"node_cost_per_month":          {Infrastructure: 1000, Supplementary: 0},
			"storage_gb_request_per_month": {Infrastructure: 0, Supplementary: 0.10},
			"storage_gb_usage_per_month":   {Infrastructure: 0, Supplementary: 0.08},
		},
		Namespaces: map[string]costdata.NamespaceCosts{
			"pipeline-ns": {
				CostModelCPUCost: 730.0,
				CostModelMemCost: 365.0,
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
}

func runNodeRecommendationsWithCost(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	costData *costdata.ClusterCostData,
) error {
	start := testutil.RecentStart()
	end := time.Now().UTC()
	digests, err := engine.QueryNodeDigests(ctx, pool, orgID, clusterUUID, start, end)
	if err != nil {
		return err
	}
	if len(digests) == 0 {
		return fmt.Errorf("no node digests found")
	}
	terms := engine.DefaultTermsForPlugin("node")
	nodeSettings, err := engine.ResolveNodeThresholdSettings(ctx, pool, orgID)
	if err != nil {
		nodeSettings = engine.DefaultNodeThresholdSettings()
	}
	cfg := engine.NodeRecConfigFromThresholds(nodeSettings)
	recs := engine.RecommendNodes(digests, cfg, nodeSettings, terms)
	if len(recs) == 0 {
		return fmt.Errorf("no node recommendations produced")
	}
	engine.ApplyNodeSavings(recs, costData, 730)
	validTerms := make([]string, len(terms))
	for i, tc := range terms {
		validTerms[i] = tc.Name
	}
	return engine.PersistNodeRecommendations(ctx, pool, orgID, clusterUUID, recs, validTerms)
}

func runPVCRecommendationsWithCost(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	costData *costdata.ClusterCostData,
) error {
	terms := engine.DefaultTermsForPlugin("pvc")
	results, err := engine.RecommendPVCs(ctx, pool, orgID, clusterUUID, terms)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("no pvc recommendations produced")
	}
	engine.ApplyPVCSavings(results, costData)
	var mediumRecs []engine.PVCRec
	for _, rec := range results {
		if rec.Term == "medium" {
			mediumRecs = append(mediumRecs, rec)
		}
	}
	if len(mediumRecs) == 0 {
		return fmt.Errorf("no medium-term pvc recommendations produced")
	}
	return engine.WritePVCRecommendations(ctx, pool, mediumRecs, []string{"medium"})
}

func runContainerRecommendations(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
) ([]engine.ContainerRec, error) {
	start := testutil.RecentStart()
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, orgID, clusterUUID, start, end, engine.OOMConfig{})
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("no container recommendations produced")
	}
	if err := engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs); err != nil {
		return nil, err
	}
	return recs, nil
}

func mediumCostCPURequest(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (int64, error) {
	var cpuReq int64
	err := pool.QueryRow(ctx, `
		SELECT rec_cpu_request_millicores FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = 'main'
		  AND term = 'medium' AND engine = 'cost'
		LIMIT 1`, orgID, clusterUUID).Scan(&cpuReq)
	return cpuReq, err
}

// TestPipeline_SavingsColumnsPopulated verifies node and PVC recommendations persist
// estimated_savings_cents when cost data is available after ingestion.
func TestPipeline_SavingsColumnsPopulated(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := regressionOrgID(t, "savings")
	clusterUUID := testutil.TestClusterUUID
	seedRegressionAccount(t, pool, orgID, clusterUUID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM node_recommendations WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM pvc_recommendation_sets WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM daily_node_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM daily_pvc_digests WHERE org_id = $1`, orgID)
	})

	costData := mockCostDataForSavings()

	require.NoError(t, ingestion.ProcessCSVToDigests(ctx, pool, strings.NewReader(buildNodeContainerCSV(7, "0.05")), orgID, clusterUUID))
	require.NoError(t, ingestion.ProcessStorageCSV(ctx, pool, strings.NewReader(buildOversizedStorageCSV(7)), orgID, clusterUUID))

	require.NoError(t, runNodeRecommendationsWithCost(ctx, pool, orgID, clusterUUID, costData))
	require.NoError(t, runPVCRecommendationsWithCost(ctx, pool, orgID, clusterUUID, costData))

	var nodeSavings *float32
	err := pool.QueryRow(ctx, `
		SELECT estimated_savings_cents FROM node_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2 AND engine = 'cost' AND term = 'medium'
		LIMIT 1`, orgID, clusterUUID).Scan(&nodeSavings)
	require.NoError(t, err)
	require.NotNil(t, nodeSavings)
	assert.Greater(t, *nodeSavings, float32(0))

	var pvcSavings *float32
	err = pool.QueryRow(ctx, `
		SELECT estimated_savings_cents FROM pvc_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND term = 'medium'
		LIMIT 1`, orgID, clusterUUID).Scan(&pvcSavings)
	require.NoError(t, err)
	require.NotNil(t, pvcSavings)
	assert.Greater(t, *pvcSavings, float32(0))
}

// TestPipeline_ThresholdOverrideUsedInRecommendation verifies a custom org threshold
// produces different recommendation output than the default after ingestion.
func TestPipeline_ThresholdOverrideUsedInRecommendation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	config.ResetForTest()
	engine.InitThresholdDefaults(config.GetConfig())
	engine.ClearThresholdSettingsCacheForTest()
	t.Cleanup(engine.ClearThresholdSettingsCacheForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := regressionOrgID(t, "threshold")
	clusterUUID := testutil.TestClusterUUID
	seedRegressionAccount(t, pool, orgID, clusterUUID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM org_recommendation_thresholds WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM recommendation_sets WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
	})

	csv := buildSpikeUsageNodeCSV(7)
	require.NoError(t, ingestion.ProcessCSVToDigests(ctx, pool, strings.NewReader(csv), orgID, clusterUUID))

	_, err := runContainerRecommendations(ctx, pool, orgID, clusterUUID)
	require.NoError(t, err)
	defaultCPU, err := mediumCostCPURequest(ctx, pool, orgID, clusterUUID)
	require.NoError(t, err)
	require.Greater(t, defaultCPU, int64(0))

	require.NoError(t, engine.UpdateThresholdSettings(ctx, pool, orgID, "container",
		json.RawMessage(`{"cpu_cost_percentile": 0.98}`)))
	engine.InvalidateThresholdCache(orgID, "container")

	_, err = runContainerRecommendations(ctx, pool, orgID, clusterUUID)
	require.NoError(t, err)
	customCPU, err := mediumCostCPURequest(ctx, pool, orgID, clusterUUID)
	require.NoError(t, err)
	assert.Greater(t, customCPU, defaultCPU,
		"higher cpu_cost_percentile (P98) should produce a larger CPU recommendation than the default (P60)")
}

// TestPipeline_DualEngineColumnsPopulated verifies node recommendations include cost and performance engines.
func TestPipeline_DualEngineColumnsPopulated(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := regressionOrgID(t, "dual-engine")
	clusterUUID := testutil.TestClusterUUID
	seedRegressionAccount(t, pool, orgID, clusterUUID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM node_recommendations WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM daily_node_digests WHERE org_id = $1`, orgID)
	})

	require.NoError(t, ingestion.ProcessCSVToDigests(ctx, pool, strings.NewReader(buildNodeContainerCSV(7, "0.05")), orgID, clusterUUID))
	require.NoError(t, runNodeRecommendationsWithCost(ctx, pool, orgID, clusterUUID, nil))

	rows, err := pool.Query(ctx, `
		SELECT DISTINCT engine FROM node_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2 AND node = 'worker-1' AND term = 'medium'`,
		orgID, clusterUUID)
	require.NoError(t, err)
	defer rows.Close()

	engines := map[string]bool{}
	for rows.Next() {
		var engineName string
		require.NoError(t, rows.Scan(&engineName))
		engines[engineName] = true
	}
	require.NoError(t, rows.Err())
	assert.True(t, engines["cost"], "expected cost engine row")
	assert.True(t, engines["performance"], "expected performance engine row")
}

// TestPipeline_BHDigestCreatesScheduleTypeRows verifies business-hours ingestion writes both schedule types.
func TestPipeline_BHDigestCreatesScheduleTypeRows(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForRegressionTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := regressionOrgID(t, "bh-digest")
	clusterUUID := testutil.TestClusterUUID
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: clusterUUID, Namespace: "",
		Timezone: "America/New_York", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "08:00", EndTime: "17:00", OffHoursWeight: 0.0, Enabled: true,
	}))

	csv := buildWeekdaySpikeCSV()
	_, err := ingestion.ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, clusterUUID)
	require.NoError(t, err)

	var allHours, businessHours int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM daily_container_digests
		WHERE org_id = $1 AND schedule_type = 'all_hours'`, orgID).Scan(&allHours)
	require.NoError(t, err)
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM daily_container_digests
		WHERE org_id = $1 AND schedule_type = 'business_hours'`, orgID).Scan(&businessHours)
	require.NoError(t, err)

	assert.Equal(t, 1, allHours)
	assert.Equal(t, 1, businessHours)
}

func buildWeekdaySpikeNodeCSV() string {
	var b strings.Builder
	b.WriteString(nodeCSVHeader + "\n")
	day := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC) // Tuesday
	for i := 0; i < 96; i++ {
		start := day.Add(time.Duration(i*15) * time.Minute)
		end := start.Add(15 * time.Minute)
		cpu := "0.95"
		if i >= 52 && i < 88 {
			cpu = "0.05"
		}
		b.WriteString(fmt.Sprintf(
			"%s,%s,bh-ns,pod-1,deploy-1,deployment,main,worker-1,"+
				"0.1,0.15,%s,0.001,134217728,134217728,104857600,100000000,0\n",
			start.Format("2006-01-02 15:04:05 +0000 UTC"),
			end.Format("2006-01-02 15:04:05 +0000 UTC"),
			cpu,
		))
	}
	return b.String()
}

// TestPipeline_BHNodeDigestCreatesScheduleTypeRows verifies cluster-level schedules
// dual-write daily_node_digests (all_hours + business_hours).
func TestPipeline_BHNodeDigestCreatesScheduleTypeRows(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	enableBusinessHoursForRegressionTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := regressionOrgID(t, "bh-node-digest")
	clusterUUID := testutil.TestClusterUUID
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_node_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})
	seedRegressionAccount(t, pool, orgID, clusterUUID)

	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: clusterUUID, Namespace: "",
		Timezone: "America/New_York", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "08:00", EndTime: "17:00", OffHoursWeight: 0.0, Enabled: true,
	}))

	_, err := ingestion.ParseAndDigestCSV(ctx, pool, strings.NewReader(buildWeekdaySpikeNodeCSV()), orgID, clusterUUID)
	require.NoError(t, err)

	var allHours, businessHours int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM daily_node_digests
		WHERE org_id = $1 AND schedule_type = 'all_hours'`, orgID).Scan(&allHours)
	require.NoError(t, err)
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM daily_node_digests
		WHERE org_id = $1 AND schedule_type = 'business_hours'`, orgID).Scan(&businessHours)
	require.NoError(t, err)

	assert.Equal(t, 1, allHours)
	assert.Equal(t, 1, businessHours)

	var allP95, bhP95 int64
	err = pool.QueryRow(ctx, `
		SELECT cpu_usage_p95_mc FROM daily_node_digests
		WHERE org_id = $1 AND node = 'worker-1' AND schedule_type = 'all_hours'`, orgID).Scan(&allP95)
	require.NoError(t, err)
	err = pool.QueryRow(ctx, `
		SELECT cpu_usage_p95_mc FROM daily_node_digests
		WHERE org_id = $1 AND node = 'worker-1' AND schedule_type = 'business_hours'`, orgID).Scan(&bhP95)
	require.NoError(t, err)
	assert.Greater(t, allP95, bhP95, "overnight spike must raise all_hours P95 above business_hours")
}
