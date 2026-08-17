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

func TestAttachGPUBusinessHours_EmitsOfficeWindowOnSizing(t *testing.T) {
	t.Parallel()
	gpuMap := map[string]*model.GPURecommendation{
		"medium": {CurrentGPUModel: "A100", GPUClassification: "well_utilized"},
	}
	recs := []*GPURec{
		{
			Term:                  "medium",
			GPUModelName:          "NVIDIA A100-SXM4-80GB",
			Classification:        GPUClassWellUtilized,
			RecommendedGPUProfile: "1g.10gb",
			CurrentGPUProfile:     "3g.40gb",
			Confidence:            0.8,
			SMActiveAvg:           0.4,
			DRAMActiveAvg:         0.3,
			TensorPipeActiveAvg:   0.2,
			FBUsageMaxMiB:         8192,
		},
	}
	attachGPUBusinessHoursToDetail(gpuMap, recs, []TermConfig{{Name: "medium", MinDataDays: 3}}, 7)

	bh := gpuMap["medium"].BusinessHours
	require.NotNil(t, bh)
	require.NotNil(t, bh.RecommendedGPUProfile)
	assert.Equal(t, "1g.10gb", *bh.RecommendedGPUProfile)
	assert.Equal(t, "NVIDIA A100-SXM4-80GB", bh.CurrentGPUModel)
	require.Contains(t, bh.Notifications, "80")
	assert.Equal(t, int16(80), bh.Notifications["80"].Code)
	assert.Empty(t, bh.Reason)
	assert.Empty(t, gpuMap["medium"].Notifications)
	assert.Empty(t, bh.Notifications["80"].SuggestedDirection)
}

func TestAttachGPUBusinessHours_InsufficientDaysOmitsCode80EvenIfRecsPresent(t *testing.T) {
	t.Parallel()
	gpuMap := map[string]*model.GPURecommendation{
		"medium": {CurrentGPUModel: "A100"},
	}
	recs := []*GPURec{
		{Term: "medium", GPUModelName: "A100", RecommendedGPUProfile: "1g.10gb"},
	}
	attachGPUBusinessHoursToDetail(gpuMap, recs, []TermConfig{{Name: "medium", MinDataDays: 3}}, 1)
	bh := gpuMap["medium"].BusinessHours
	require.NotNil(t, bh)
	assert.Nil(t, bh.RecommendedGPUProfile)
	assert.Empty(t, bh.Notifications)
	assert.Contains(t, bh.Reason, "insufficient business hours data")
}

func TestAttachGPUBusinessHours_InsufficientReasonOmitsCode80(t *testing.T) {
	t.Parallel()
	gpuMap := map[string]*model.GPURecommendation{
		"medium": {CurrentGPUModel: "A100"},
	}
	attachGPUBusinessHoursToDetail(gpuMap, nil, []TermConfig{{Name: "medium", MinDataDays: 3, WindowDays: 7}}, 1)
	bh := gpuMap["medium"].BusinessHours
	require.NotNil(t, bh)
	assert.Nil(t, bh.RecommendedGPUProfile)
	assert.Empty(t, bh.Notifications)
	assert.Contains(t, bh.Reason, "insufficient business hours data")
}

func TestEnrichContainerDetailGPUWithBusinessHours_KillSwitch(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "false")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)

	result := &model.NativeContainerResult{
		ClusterUUID: "cluster",
		Project:     "ns",
		Workload:    "wl",
		Container:   "ctr",
		GPU: map[string]*model.GPURecommendation{
			"medium": {CurrentGPUModel: "A100"},
		},
	}
	err := EnrichContainerDetailGPUWithBusinessHours(context.Background(), nil, "org", result)
	require.NoError(t, err)
	assert.Nil(t, result.GPU["medium"].BusinessHours)
}

func TestNativeContainerListJSON_OmitsGPUBusinessHours(t *testing.T) {
	t.Parallel()
	rec := model.NativeContainerResult{
		Container: "ctr",
		GPU: map[string]*model.GPURecommendation{
			"medium": {CurrentGPUModel: "A100", GPUClassification: "idle"},
		},
	}
	raw, err := json.Marshal(rec)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "business_hours")
}
