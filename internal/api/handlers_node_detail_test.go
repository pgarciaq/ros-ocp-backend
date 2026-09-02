package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

func TestNodeUtilizationDetailFromRec_FlattensPrimaryFields(t *testing.T) {
	rec := model.NodeUtilizationRec{
		Node:                  "worker-1",
		ClusterUUID:           "cluster-uuid",
		InstanceType:          "m5.xlarge",
		MachineSetName:        "worker-us-east-1a",
		SuggestedInstanceType: "c5.xlarge",
		InstanceTypeReason:    "CPU-stranded node",
		PodCount:              85,
		Classification: model.NodeUtilizationClassification{
			Category: "stranded_cpu",
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
	assert.Equal(t, "stranded_cpu", detail.Category)
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
			MemUsageP50KiB:       4194304,
			MemUsageP95KiB:       6291456,
			MaxCPUAllocatableMC:  8000,
			MaxMemAllocatableKiB: 16777216,
			MaxCPURequestsMC:     7200,
			MaxMemRequestsKiB:    12582912,
		},
		{
			BucketDate:           "2026-06-16",
			CPUUsageP50MC:        2800,
			CPUUsageP95MC:        4900,
			MemUsageP50KiB:       3932160,
			MemUsageP95KiB:       5898240,
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
		Node:                "worker-1",
		ClusterUUID:         "cluster-uuid",
		RecommendationTerms: map[string]model.NodeUtilizationTermRec{},
	}

	detail := nodeUtilizationDetailFromRec(rec)
	assert.Nil(t, detail.DailyDigests)
}

func TestNodeUtilizationDetailRec_BusinessHoursDigestsOmittedWhenEmpty(t *testing.T) {
	detail := model.NodeUtilizationDetailRec{Node: "worker-1", ClusterUUID: "cluster-uuid"}
	raw, err := json.Marshal(detail)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "daily_digests_business_hours")
	assert.NotContains(t, string(raw), "daily_digests")
}

func TestNodeDigestRowsToDailyItems_MapsMaxColumns(t *testing.T) {
	day, err := time.Parse("2006-01-02", "2026-09-01")
	require.NoError(t, err)
	allocCPU := int64(8000)
	allocMem := int64(33554432)
	rows := []engine.NodeDigestRow{
		{
			BucketDate:        day,
			CPUUsageP50MC:     100,
			CPUUsageP95MC:     200,
			CPUUsageMaxMC:     300,
			MemUsageP50KiB:    1000,
			MemUsageP95KiB:    2000,
			MemUsageMaxKiB:    3000,
			MaxCPUAllocMC:     &allocCPU,
			MaxMemAllocKiB:    &allocMem,
			MaxCPURequestsMC:  400,
			MaxMemRequestsKiB: 500,
		},
		{
			BucketDate:     day.AddDate(0, 0, 1),
			CPUUsageP50MC:  50,
			CPUUsageMaxMC:  0,
			MemUsageMaxKiB: 0,
		},
	}
	items := nodeDigestRowsToDailyItems(rows)
	require.Len(t, items, 2)
	assert.Equal(t, "2026-09-01", items[0].BucketDate)
	require.NotNil(t, items[0].CPUUsageMaxMC)
	assert.Equal(t, int64(300), *items[0].CPUUsageMaxMC)
	require.NotNil(t, items[0].MemUsageMaxKiB)
	assert.Equal(t, int64(3000), *items[0].MemUsageMaxKiB)
	assert.Equal(t, int64(8000), items[0].MaxCPUAllocatableMC)
	assert.Equal(t, int64(33554432), items[0].MaxMemAllocatableKiB)
	assert.Nil(t, items[1].CPUUsageMaxMC)
	assert.Nil(t, items[1].MemUsageMaxKiB)
}
