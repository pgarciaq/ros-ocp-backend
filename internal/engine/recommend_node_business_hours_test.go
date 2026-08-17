package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
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
