package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

func TestNodeUtilizationDetailFromRec_FlattensPrimaryFields(t *testing.T) {
	stranded := "cpu"
	rec := model.NodeUtilizationRec{
		Node:                  "worker-1",
		ClusterUUID:           "cluster-uuid",
		InstanceType:          "m5.xlarge",
		MachineSetName:        "worker-us-east-1a",
		SuggestedInstanceType: "c5.xlarge",
		InstanceTypeReason:    "CPU-stranded node",
		PodCount:              85,
		Classification: model.NodeUtilizationClassification{
			IdleState:        "active",
			StrandedResource: &stranded,
		},
		Metrics: model.NodeUtilizationMetrics{CPUUtilP95: 0.2, MemUtilP95: 0.8},
		RecommendationTerms: map[string]model.NodeUtilizationTermRec{
			"medium_term": {
				RecommendationEngines: &model.NodeUtilizationEngines{
					Cost: &model.NodeUtilizationEngineRec{
						Notifications: map[string]notifications.NotificationEntry{
							"stranded_resources": {Code: 12},
						},
					},
				},
			},
		},
	}

	detail := nodeUtilizationDetailFromRec(rec)
	assert.Equal(t, "worker-1", detail.Node)
	assert.Equal(t, "worker-us-east-1a", detail.MachineSetName)
	assert.Equal(t, "c5.xlarge", detail.SuggestedInstanceType)
	assert.Equal(t, "active", detail.IdleState)
	require.NotNil(t, detail.Notifications)
	assert.Contains(t, detail.Notifications, "stranded_resources")
}

func TestNodeUtilizationDetailFromRec_AggregatesAllTermsAndEngines(t *testing.T) {
	rec := model.NodeUtilizationRec{
		RecommendationTerms: map[string]model.NodeUtilizationTermRec{
			"medium_term": {
				RecommendationEngines: &model.NodeUtilizationEngines{
					Cost: &model.NodeUtilizationEngineRec{
						Notifications: map[string]notifications.NotificationEntry{
							"stranded_resources": {Code: 12},
						},
					},
				},
			},
			"short_term": {
				RecommendationEngines: &model.NodeUtilizationEngines{
					Performance: &model.NodeUtilizationEngineRec{
						Notifications: map[string]notifications.NotificationEntry{
							"node_underutilized": {Code: 11},
						},
					},
				},
			},
			"long_term": {
				RecommendationEngines: &model.NodeUtilizationEngines{
					Cost: &model.NodeUtilizationEngineRec{
						Notifications: map[string]notifications.NotificationEntry{
							"no_cost_data": {Code: 25},
						},
					},
				},
			},
		},
	}

	detail := nodeUtilizationDetailFromRec(rec)
	require.NotNil(t, detail.Notifications)
	assert.Contains(t, detail.Notifications, "stranded_resources")
	assert.Contains(t, detail.Notifications, "node_underutilized")
	assert.Contains(t, detail.Notifications, "no_cost_data")
}

func TestNodeUtilizationDetailFromRec_DeduplicatesBySeverity(t *testing.T) {
	rec := model.NodeUtilizationRec{
		RecommendationTerms: map[string]model.NodeUtilizationTermRec{
			"long_term": {
				RecommendationEngines: &model.NodeUtilizationEngines{
					Cost: &model.NodeUtilizationEngineRec{
						Notifications: map[string]notifications.NotificationEntry{
							"overcommit": {Code: 12, Type: "WARNING"},
						},
					},
				},
			},
			"short_term": {
				RecommendationEngines: &model.NodeUtilizationEngines{
					Cost: &model.NodeUtilizationEngineRec{
						Notifications: map[string]notifications.NotificationEntry{
							"overcommit": {Code: 3, Type: "CRITICAL"},
						},
					},
				},
			},
		},
	}

	detail := nodeUtilizationDetailFromRec(rec)
	require.NotNil(t, detail.Notifications)
	assert.Equal(t, int16(3), detail.Notifications["overcommit"].Code)
}

func TestNodeDailyDigestItem_SerializesCorrectly(t *testing.T) {
	digests := []model.NodeDailyDigestItem{
		{
			BucketDate:           "2026-06-15",
			CPUUsageP50MC:        3200,
			CPUUsageP95MC:        5600,
			MemUsageP50KiB:      4194304,
			MemUsageP95KiB:      6291456,
			MaxCPUAllocatableMC:  8000,
			MaxMemAllocatableKiB: 16777216,
			MaxCPURequestsMC:     7200,
			MaxMemRequestsKiB:    12582912,
		},
		{
			BucketDate:           "2026-06-16",
			CPUUsageP50MC:        2800,
			CPUUsageP95MC:        4900,
			MemUsageP50KiB:      3932160,
			MemUsageP95KiB:      5898240,
			MaxCPUAllocatableMC:  8000,
			MaxMemAllocatableKiB: 16777216,
			MaxCPURequestsMC:     6800,
			MaxMemRequestsKiB:    11534336,
		},
	}

	assert.Len(t, digests, 2)
	assert.Equal(t, "2026-06-15", digests[0].BucketDate)
	assert.Equal(t, int64(5600), digests[0].CPUUsageP95MC)
	assert.Equal(t, int64(6291456), digests[0].MemUsageP95KiB)
	assert.Equal(t, int64(8000), digests[0].MaxCPUAllocatableMC)
	assert.Equal(t, int64(16777216), digests[0].MaxMemAllocatableKiB)
	assert.Equal(t, int64(7200), digests[0].MaxCPURequestsMC)
	assert.Equal(t, int64(12582912), digests[0].MaxMemRequestsKiB)
}

func TestNodeUtilizationDetailRec_DailyDigestsOmittedWhenEmpty(t *testing.T) {
	rec := model.NodeUtilizationRec{
		Node:        "worker-1",
		ClusterUUID: "cluster-uuid",
		RecommendationTerms: map[string]model.NodeUtilizationTermRec{},
	}

	detail := nodeUtilizationDetailFromRec(rec)
	assert.Nil(t, detail.DailyDigests)
}
