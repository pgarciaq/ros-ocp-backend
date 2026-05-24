package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func enableBHForCompoundTest(t *testing.T) {
	t.Helper()
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	config.ResetForTest()
}

func seedBHSchedule(t *testing.T, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, UpsertBusinessHoursSchedule(ctx, pool, BusinessHoursSchedule{
		OrgID:       orgID,
		ClusterUUID: OrgClusterSentinelUUID,
		Timezone:    "UTC",
		Days:        []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime:   "00:00",
		EndTime:     "23:59",
		Enabled:     true,
	}))
}

func seedBHDigestPair(t *testing.T, pool *pgxpool.Pool, orgID string, days int) {
	t.Helper()
	start := testutil.RecentStart()
	for i := 0; i < days; i++ {
		base := testutil.ContainerDigestRow{
			BucketDate:       start.AddDate(0, 0, i),
			OrgID:            orgID,
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        testutil.TestNamespace,
			Workload:         testutil.TestWorkload,
			WorkloadType:     testutil.TestWorkloadType,
			ContainerName:    testutil.TestContainer,
			CPURequestP50MC:  5000,
			CPURequestP95MC:  5500,
			CPUUsageP50MC:    2000,
			CPUUsageP60MC:    3000,
			CPUUsageP95MC:    4000,
			CPUUsageP98MC:    4500,
			CPUUsageP99MC:    4600,
			CPUUsageMaxMC:    5000,
			MemRequestP50KiB: 524288,
			MemRequestP95KiB: 550000,
			MemUsageP95KiB:   500000,
			SampleCount:      96,
		}
		allHours := base
		allHours.ScheduleType = "all_hours"
		allHours.CPUUsageP50MC = 4500
		allHours.CPUUsageP98MC = 4800
		testutil.SeedContainerDigest(t, pool, allHours)

		bh := base
		bh.ScheduleType = "business_hours"
		testutil.SeedContainerDigest(t, pool, bh)
	}
}

func runBHCostCPU(t *testing.T, pool *pgxpool.Pool, orgID string) int64 {
	t.Helper()
	ctx := context.Background()
	start := testutil.RecentStart()
	end := start.AddDate(0, 0, 6)

	recs, err := RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, start, end, OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)

	native := buildNativeFromContainerRecs(recs)
	require.NoError(t, EnrichNativeContainerResultsWithBusinessHours(ctx, pool, orgID, native))

	term := native[0].Recommendations["short_term"]
	require.NotNil(t, term.Cost)
	require.NotNil(t, term.Cost.BusinessHours)
	require.NotNil(t, term.Cost.BusinessHours.CPURequestMillicores,
		"business hours cost recommendation should be present when BH schedule and digests exist")
	return *term.Cost.BusinessHours.CPURequestMillicores
}

func buildNativeFromContainerRecs(recs []ContainerRec) []model.NativeContainerResult {
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

func TestBH_WithCustomThresholds_UsesCustomPercentile(t *testing.T) {
	enableBHForCompoundTest(t)
	pool, ctx, orgID := setupRecalcCorrectnessTest(t)
	seedBHSchedule(t, pool, orgID)
	seedBHDigestPair(t, pool, orgID, 7)

	require.NoError(t, UpdateThresholdSettings(ctx, pool, orgID, "container",
		json.RawMessage(`{"cpu_cost_percentile": 0.50}`)))

	bhCPU := runBHCostCPU(t, pool, orgID)
	assert.True(t, bhCPU >= 1500 && bhCPU <= 2500,
		"BH cost recommendation with cpu_cost_percentile=0.50 should use P50 (~2000mc), got %d", bhCPU)
}

func TestBH_WithDefaultThresholds_UsesDefaultPercentile(t *testing.T) {
	enableBHForCompoundTest(t)
	pool, _, orgID := setupRecalcCorrectnessTest(t)
	seedBHSchedule(t, pool, orgID)
	seedBHDigestPair(t, pool, orgID, 7)

	bhCPU := runBHCostCPU(t, pool, orgID)
	defaultPercentile := DefaultContainerSizingThresholds().CPUCostPercentile
	assert.True(t, bhCPU >= 2500,
		"BH cost recommendation with default cpu_cost_percentile=%.2f should exceed P50 (~2000mc), got %d",
		defaultPercentile, bhCPU)
	assert.True(t, bhCPU <= 5000,
		"BH cost recommendation with default percentile should not exceed all-hours P50 (~4500mc), got %d", bhCPU)
}

func TestBH_ThresholdChange_TriggersRecalc_NotReship(t *testing.T) {
	enableBHForCompoundTest(t)
	config.ResetForTest()
	t.Setenv("ROS_THRESHOLD_RECALCULATION_ENABLED", "true")

	pool, ctx, orgID := setupRecalcCorrectnessTest(t)
	clusterUUID := testutil.TestClusterUUID
	seedBHSchedule(t, pool, orgID)
	require.NoError(t, UpsertBusinessHoursSchedule(ctx, pool, BusinessHoursSchedule{
		OrgID:       orgID,
		ClusterUUID: testutil.TestClusterUUID,
		Namespace:   "",
		Timezone:    "UTC",
		Days:        []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime:   "00:00",
		EndTime:     "23:59",
		Enabled:     true,
	}))

	_, err := pool.Exec(ctx, `
		UPDATE business_hours_schedules
		SET reship_forward_only_since = NOW() - interval '1 hour'
		WHERE org_id = $1 AND cluster_uuid = $2::uuid`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	var forwardOnlyBefore *time.Time
	err = pool.QueryRow(ctx, `
		SELECT reship_forward_only_since FROM business_hours_schedules
		WHERE org_id = $1 AND cluster_uuid = $2::uuid`,
		orgID, testutil.TestClusterUUID).Scan(&forwardOnlyBefore)
	require.NoError(t, err)
	require.NotNil(t, forwardOnlyBefore, "fixture should start with reship_forward_only_since set")

	var recalcTriggered bool
	var recalcType string
	SetThresholdRecalcHookForTest(func(oid, rt string) {
		recalcTriggered = true
		recalcType = rt
	})
	defer ClearThresholdRecalcHookForTest()

	require.NoError(t, UpdateThresholdSettings(ctx, pool, orgID, "container",
		json.RawMessage(`{"cpu_cost_percentile": 0.55}`)))
	TriggerThresholdRecalculationAsync(pool, orgID, "container")

	assert.True(t, recalcTriggered,
		"threshold change should trigger async recalculation, not masu reship")
	assert.Equal(t, "container", recalcType,
		"recalculation hook should receive the container recommendation type")

	pending, err := reshipPendingSince(ctx, pool, orgID, clusterUUID)
	require.NoError(t, err)
	assert.Nil(t, pending, "threshold update must not trigger masu reship (reship mock would not be called)")

	var forwardOnlyAfter *time.Time
	err = pool.QueryRow(ctx, `
		SELECT reship_forward_only_since FROM business_hours_schedules
		WHERE org_id = $1 AND cluster_uuid = $2::uuid`,
		orgID, testutil.TestClusterUUID).Scan(&forwardOnlyAfter)
	require.NoError(t, err)
	require.NotNil(t, forwardOnlyAfter,
		"reship_forward_only_since must remain set after threshold update (no reship triggered)")
	assert.True(t, forwardOnlyBefore.Equal(*forwardOnlyAfter),
		"reship_forward_only_since timestamp should remain unchanged after threshold update")
}

func reshipPendingSince(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (*time.Time, error) {
	var pending *time.Time
	err := pool.QueryRow(ctx, `
		SELECT reship_pending_since FROM business_hours_schedules
		WHERE org_id = $1 AND cluster_uuid = $2::uuid`,
		orgID, clusterUUID).Scan(&pending)
	return pending, err
}
