package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgdigest"
)

func TestAttachNodeBusinessHours_EmitsNotPeakSafeOnSizing(t *testing.T) {
	t.Parallel()
	detail := &model.NodeUtilizationDetailRec{
		RecommendationTerms: map[string]model.NodeUtilizationTermRec{
			"medium_term": {
				RecommendationEngines: &model.NodeUtilizationEngines{
					Cost:        &model.NodeUtilizationEngineRec{RecommendedCPUCores: 8},
					Performance: &model.NodeUtilizationEngineRec{RecommendedCPUCores: 10},
				},
			},
		},
	}
	recs := []NodeRec{
		{Node: "worker-1", Term: "medium", Engine: "cost", RecommendedCPUMC: 4000, RecommendedMemKiB: 8 * 1024 * 1024},
		{Node: "worker-1", Term: "medium", Engine: "performance", RecommendedCPUMC: 5000, RecommendedMemKiB: 10 * 1024 * 1024},
	}
	attachNodeBusinessHoursToDetail(detail, recs, []TermConfig{{Name: "medium", MinDataDays: 3}}, 7)

	costBH := detail.RecommendationTerms["medium_term"].RecommendationEngines.Cost.BusinessHours
	require.NotNil(t, costBH)
	require.NotNil(t, costBH.RecommendedCPUCores)
	assert.InDelta(t, 4.0, float64(*costBH.RecommendedCPUCores), 0.001)
	require.NotNil(t, costBH.RecommendedMemoryGiB)
	assert.InDelta(t, 8.0, float64(*costBH.RecommendedMemoryGiB), 0.001)
	require.Contains(t, costBH.Notifications, "79")
	assert.Equal(t, int16(79), costBH.Notifications["79"].Code)
	assert.Empty(t, costBH.Reason)
	assert.Empty(t, detail.RecommendationTerms["medium_term"].RecommendationEngines.Cost.Notifications)

	perfBH := detail.RecommendationTerms["medium_term"].RecommendationEngines.Performance.BusinessHours
	require.NotNil(t, perfBH)
	require.Contains(t, perfBH.Notifications, "79")
}

func TestAttachNodeBusinessHours_InsufficientDaysOmitsCode79EvenIfRecsPresent(t *testing.T) {
	t.Parallel()
	detail := &model.NodeUtilizationDetailRec{
		RecommendationTerms: map[string]model.NodeUtilizationTermRec{
			"medium_term": {
				RecommendationEngines: &model.NodeUtilizationEngines{
					Cost: &model.NodeUtilizationEngineRec{RecommendedCPUCores: 8},
				},
			},
		},
	}
	recs := []NodeRec{
		{Node: "worker-1", Term: "medium", Engine: "cost", RecommendedCPUMC: 4000, RecommendedMemKiB: 8 * 1024 * 1024},
	}
	attachNodeBusinessHoursToDetail(detail, recs, []TermConfig{{Name: "medium", MinDataDays: 3}}, 1)
	bh := detail.RecommendationTerms["medium_term"].RecommendationEngines.Cost.BusinessHours
	require.NotNil(t, bh)
	assert.Nil(t, bh.RecommendedCPUCores)
	assert.Empty(t, bh.Notifications)
	assert.Contains(t, bh.Reason, "insufficient business hours data")
}

func TestAttachNodeBusinessHours_InsufficientReasonOmitsCode79(t *testing.T) {
	t.Parallel()
	detail := &model.NodeUtilizationDetailRec{
		RecommendationTerms: map[string]model.NodeUtilizationTermRec{
			"medium_term": {
				RecommendationEngines: &model.NodeUtilizationEngines{
					Cost: &model.NodeUtilizationEngineRec{RecommendedCPUCores: 8},
				},
			},
		},
	}
	attachNodeBusinessHoursToDetail(detail, nil, []TermConfig{{Name: "medium", MinDataDays: 3, WindowDays: 7}}, 1)
	bh := detail.RecommendationTerms["medium_term"].RecommendationEngines.Cost.BusinessHours
	require.NotNil(t, bh)
	assert.Nil(t, bh.RecommendedCPUCores)
	assert.Empty(t, bh.Notifications)
	assert.Contains(t, bh.Reason, "insufficient business hours data")
}

func TestEnrichNodeDetailWithBusinessHours_KillSwitch(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "false")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)

	detail := &model.NodeUtilizationDetailRec{
		RecommendationTerms: map[string]model.NodeUtilizationTermRec{
			"medium_term": {
				RecommendationEngines: &model.NodeUtilizationEngines{
					Cost: &model.NodeUtilizationEngineRec{RecommendedCPUCores: 8},
				},
			},
		},
	}
	err := EnrichNodeDetailWithBusinessHours(context.Background(), nil, "org", "cluster", "worker-1", detail)
	require.NoError(t, err)
	assert.Nil(t, detail.RecommendationTerms["medium_term"].RecommendationEngines.Cost.BusinessHours)
}

func TestFilterNodeDigestsByInclusiveRange(t *testing.T) {
	t.Parallel()
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		require.NoError(t, err)
		return d
	}
	rows := []NodeDigestRow{
		{BucketDate: day("2026-09-01"), Node: "n1", CPUUsageP50MC: 1},
		{BucketDate: day("2026-09-05"), Node: "n1", CPUUsageP50MC: 2},
		{BucketDate: day("2026-09-10"), Node: "n1", CPUUsageP50MC: 3},
		{BucketDate: day("2026-09-15"), Node: "n1", CPUUsageP50MC: 4},
	}
	got := FilterNodeDigestsByInclusiveRange(rows, day("2026-09-05"), day("2026-09-10"))
	require.Len(t, got, 2)
	assert.Equal(t, int64(2), got[0].CPUUsageP50MC)
	assert.Equal(t, int64(3), got[1].CPUUsageP50MC)

	openStart := FilterNodeDigestsByInclusiveRange(rows, time.Time{}, day("2026-09-05"))
	require.Len(t, openStart, 2)
	openEnd := FilterNodeDigestsByInclusiveRange(rows, day("2026-09-10"), time.Time{})
	require.Len(t, openEnd, 2)
}

func TestQueryNodeBHDetailDigests_CoverExpandsAndMapsMax(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	InvalidateTermCache("org-bh-517", "node")

	orgID := "org-bh-517"
	clusterUUID := "51751751-7517-7517-7517-517517517517"
	nodeName := "bh-detail-dup-node"
	maxDay := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	inWindow := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	farDay := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	for _, d := range []time.Time{farDay, inWindow, maxDay} {
		month := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
		require.NoError(t, pgdigest.EnsureRangePartition(ctx, pool, "daily_node_digests", month))
	}

	insert := func(day time.Time, schedule string, cpuMax, memMax int64) {
		t.Helper()
		_, err := pool.Exec(ctx, `
			INSERT INTO daily_node_digests (
				bucket_date, org_id, cluster_uuid, node, schedule_type,
				cpu_usage_p50_mc, cpu_usage_p95_mc, cpu_usage_max_mc,
				mem_usage_p50_kib, mem_usage_p95_kib, mem_usage_max_kib,
				max_cpu_allocatable_mc, max_mem_allocatable_kib,
				max_cpu_requests_mc, max_mem_requests_kib, sample_count
			) VALUES ($1, $2, $3::uuid, $4, $5, 100, 200, $6, 1000, 2000, $7, 8000, 33554432, 400, 500, 24)`,
			day, orgID, clusterUUID, nodeName, schedule, cpuMax, memMax)
		require.NoError(t, err)
	}
	insert(maxDay, "business_hours", 300, 3000)
	insert(inWindow, "business_hours", 250, 2500)
	insert(farDay, "business_hours", 111, 1111)
	insert(maxDay, "all_hours", 999, 9999)

	rows, enrichStart, enrichEnd, err := QueryNodeBHDetailDigests(ctx, pool, orgID, clusterUUID, nodeName, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Equal(t, maxDay, enrichEnd)
	require.True(t, !enrichStart.After(inWindow), "enrich window should include in-window day")
	require.True(t, farDay.Before(enrichStart), "far day should sit outside default enrich window")
	require.Len(t, rows, 2)
	var sawMax bool
	for _, r := range rows {
		assert.NotEqual(t, farDay.Format("2006-01-02"), r.BucketDate.Format("2006-01-02"))
		if r.BucketDate.Format("2006-01-02") == maxDay.Format("2006-01-02") {
			sawMax = true
			assert.Equal(t, int64(300), r.CPUUsageMaxMC)
			assert.Equal(t, int64(3000), r.MemUsageMaxKiB)
		}
	}
	require.True(t, sawMax)

	covered, coverEnrichStart, coverEnrichEnd, err := QueryNodeBHDetailDigests(ctx, pool, orgID, clusterUUID, nodeName, farDay, maxDay)
	require.NoError(t, err)
	assert.Equal(t, enrichStart, coverEnrichStart)
	assert.Equal(t, enrichEnd, coverEnrichEnd)
	require.Len(t, covered, 3)
	chart := FilterNodeDigestsByInclusiveRange(covered, farDay, farDay)
	require.Len(t, chart, 1)
	assert.Equal(t, int64(111), chart[0].CPUUsageMaxMC)
}

func TestNodeUtilizationListJSON_OmitsBusinessHours(t *testing.T) {
	t.Parallel()
	rec := model.NodeUtilizationRec{
		Node: "worker-1",
		RecommendationTerms: map[string]model.NodeUtilizationTermRec{
			"medium_term": {
				RecommendationEngines: &model.NodeUtilizationEngines{
					Cost: &model.NodeUtilizationEngineRec{RecommendedCPUCores: 8},
				},
			},
		},
	}
	raw, err := json.Marshal(rec)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "business_hours")
}
