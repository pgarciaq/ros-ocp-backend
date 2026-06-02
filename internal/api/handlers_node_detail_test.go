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
