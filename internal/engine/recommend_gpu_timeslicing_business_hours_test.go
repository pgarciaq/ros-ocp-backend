package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func officeClusterSchedule() bhschedule.Schedule {
	return bhschedule.Schedule{
		Timezone: "UTC", Days: []string{"monday", "tuesday"},
		StartTime: "09:00", EndTime: "17:00", Enabled: true,
	}
}

func TestSchedulesWindowEqual_IgnoresIdentityFields(t *testing.T) {
	t.Parallel()
	a := officeClusterSchedule()
	a.OrgID = "org-a"
	a.Namespace = ""
	b := officeClusterSchedule()
	b.OrgID = "org-b"
	b.Namespace = "ml"
	b.Days = []string{"tuesday", "monday"}
	assert.True(t, schedulesWindowEqual(a, b))

	b.StartTime = "08:00"
	assert.False(t, schedulesWindowEqual(a, b))
}

func TestTimeslicingGroupMatchesClusterWindow_Homogeneous(t *testing.T) {
	t.Parallel()
	cluster := officeClusterSchedule()
	cache := bhschedule.NewCacheForTest(&cluster, &cluster, nil)
	group := NodeGPUGroup{
		NodeName: "gpu-1", GPUModel: "T4", Term: "medium",
		Containers: []NodeGPUContainer{
			{Namespace: "ml-team"},
			{Namespace: "serving"},
		},
	}
	assert.True(t, timeslicingGroupMatchesClusterWindow(group, cache, cache.ResolveCluster()))
}

func TestTimeslicingGroupMatchesClusterWindow_HeterogeneousOmits(t *testing.T) {
	t.Parallel()
	cluster := officeClusterSchedule()
	batch := officeClusterSchedule()
	batch.StartTime = "00:00"
	batch.EndTime = "06:00"
	cache := bhschedule.NewCacheForTest(&cluster, &cluster, map[string]bhschedule.Schedule{
		"batch": batch,
	})
	group := NodeGPUGroup{
		NodeName: "gpu-1", GPUModel: "T4", Term: "medium",
		Containers: []NodeGPUContainer{
			{Namespace: "ml-team"},
			{Namespace: "batch"},
		},
	}
	assert.False(t, timeslicingGroupMatchesClusterWindow(group, cache, cache.ResolveCluster()))
}

func TestAttachTimeslicingBusinessHours_EmitsClusterWindowOnSizing(t *testing.T) {
	t.Parallel()
	rec := &model.NodeGPURecommendation{
		NodeName: "gpu-1", GPUModel: "T4", Term: "medium", RecommendedReplicas: 4,
	}
	tsRec := &TimeslicingRec{
		RecommendedReplicas: 8,
		Confidence:          0.62,
		CandidateContainers: []GPUContainerRef{{Namespace: "ml"}, {Namespace: "ml"}},
		ImpactedContainers:  []GPUContainerRef{{Namespace: "ml"}},
	}
	attachTimeslicingBusinessHours(rec, tsRec, TermConfig{Name: "medium", MinDataDays: 3}, 7, NodeGPUGroup{})

	bh := rec.BusinessHours
	require.NotNil(t, bh)
	require.NotNil(t, bh.RecommendedReplicas)
	assert.Equal(t, 8, *bh.RecommendedReplicas)
	require.NotNil(t, bh.Confidence)
	assert.InDelta(t, 0.62, float64(*bh.Confidence), 0.001)
	require.NotNil(t, bh.CandidateCount)
	assert.Equal(t, 2, *bh.CandidateCount)
	require.NotNil(t, bh.ImpactedCount)
	assert.Equal(t, 1, *bh.ImpactedCount)
	require.Contains(t, bh.Notifications, "81")
	assert.Equal(t, int16(81), bh.Notifications["81"].Code)
	assert.Empty(t, bh.Reason)
	assert.Empty(t, rec.NotificationCodes)
	assert.Nil(t, rec.EstimatedMonthlySavings)
}

func TestAttachTimeslicingBusinessHours_InsufficientDaysOmitsCode81(t *testing.T) {
	t.Parallel()
	rec := &model.NodeGPURecommendation{NodeName: "gpu-1", Term: "medium"}
	tsRec := &TimeslicingRec{RecommendedReplicas: 4, CandidateContainers: []GPUContainerRef{{}}}
	attachTimeslicingBusinessHours(rec, tsRec, TermConfig{Name: "medium", MinDataDays: 3}, 1, NodeGPUGroup{})
	bh := rec.BusinessHours
	require.NotNil(t, bh)
	assert.Nil(t, bh.RecommendedReplicas)
	assert.Empty(t, bh.Notifications)
	assert.Contains(t, bh.Reason, "insufficient business hours data")
}

func TestAttachTimeslicingBusinessHours_NilRecOmitsCode81(t *testing.T) {
	t.Parallel()
	rec := &model.NodeGPURecommendation{NodeName: "gpu-1", Term: "medium"}
	attachTimeslicingBusinessHours(rec, nil, TermConfig{Name: "medium", MinDataDays: 3, WindowDays: 7}, 7, NodeGPUGroup{})
	bh := rec.BusinessHours
	require.NotNil(t, bh)
	assert.Nil(t, bh.RecommendedReplicas)
	assert.Empty(t, bh.Notifications)
	assert.Contains(t, bh.Reason, "insufficient business hours data")
}

func TestEnrichNodeGPUTimeslicingDetailWithBusinessHours_KillSwitch(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "false")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)

	recs := []model.NodeGPURecommendation{{
		NodeName: "gpu-1", ClusterUUID: "cluster", GPUModel: "T4", Term: "medium",
	}}
	err := EnrichNodeGPUTimeslicingDetailWithBusinessHours(context.Background(), nil, "org", "gpu-1", recs)
	require.NoError(t, err)
	assert.Nil(t, recs[0].BusinessHours)
}

func TestNodeGPURecommendationListJSON_OmitsBusinessHours(t *testing.T) {
	t.Parallel()
	rec := model.NodeGPURecommendation{
		NodeName: "gpu-1", GPUModel: "T4", Term: "medium", RecommendedReplicas: 4,
	}
	raw, err := json.Marshal(rec)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "business_hours")
	assert.NotContains(t, string(raw), "GPU_TS_BH_CLUSTER_WINDOW")
}

func TestAttachTimeslicingBusinessHours_CopiesCandidateUtilization(t *testing.T) {
	t.Parallel()
	rec := &model.NodeGPURecommendation{
		NodeName: "gpu-1", GPUModel: "T4", Term: "medium",
	}
	tsRec := &TimeslicingRec{
		RecommendedReplicas: 4,
		Confidence:          0.5,
		CandidateContainers: []GPUContainerRef{
			{Namespace: "ml", Workload: "train", Container: "gpu"},
		},
	}
	group := NodeGPUGroup{
		GPUModel: "T4",
		Containers: []NodeGPUContainer{
			{
				Namespace: "ml", Workload: "train", Container: "gpu",
				Rec: &GPURec{
					SMActiveAvg: 0.20, DRAMActiveAvg: 0.10,
					TensorPipeActiveAvg: 0.15, FBUsageMaxMiB: 2048,
				},
			},
			{
				Namespace: "other", Workload: "batch", Container: "gpu",
				Rec: &GPURec{SMActiveAvg: 0.90, FBUsageMaxMiB: 16000},
			},
		},
	}
	attachTimeslicingBusinessHours(rec, tsRec, TermConfig{Name: "medium", MinDataDays: 3}, 7, group)
	bh := rec.BusinessHours
	require.NotNil(t, bh)
	assert.InDelta(t, 0.20, float64(bh.SMActiveAvg), 1e-6)
	assert.InDelta(t, 0.10, float64(bh.DRAMActiveAvg), 1e-6)
	assert.InDelta(t, 0.15, float64(bh.TensorPipeActiveAvg), 1e-6)
	assert.InDelta(t, 2048, float64(bh.FBUsageMaxMiB), 1e-6)
	require.NotNil(t, bh.TotalFBMiB)
	assert.Equal(t, int64(16384), *bh.TotalFBMiB)
}
