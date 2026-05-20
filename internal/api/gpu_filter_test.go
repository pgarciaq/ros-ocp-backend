package api

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/stretchr/testify/assert"
)

func makeTestResults() []model.NativeContainerResult {
	return []model.NativeContainerResult{
		{Container: "no-gpu", GPU: nil},
		{Container: "a100-idle", GPU: map[string]*model.GPURecommendation{
			"medium": {
				CurrentGPUModel:   "NVIDIA A100-SXM4-80GB",
				GPUClassification: "idle",
			},
		}},
		{Container: "t4-underutil", GPU: map[string]*model.GPURecommendation{
			"medium": {
				CurrentGPUModel:   "Tesla T4",
				GPUClassification: "underutilized",
			},
		}},
		{Container: "h100-well", GPU: map[string]*model.GPURecommendation{
			"medium": {
				CurrentGPUModel:   "NVIDIA H100",
				GPUClassification: "well_utilized",
			},
		}},
	}
}

func TestFilterGPUResults_IsPassthrough(t *testing.T) {
	results := makeTestResults()

	out, count := filterGPUResults(results, 100, nil, nil)
	assert.Equal(t, 100, count, "passthrough preserves original totalCount")
	assert.Len(t, out, 4)

	out, count = filterGPUResults(results, 100, []string{"A100"}, nil)
	assert.Equal(t, 100, count, "gpu_model filter is now handled at SQL level; filterGPUResults is a passthrough")
	assert.Len(t, out, 4)

	out, count = filterGPUResults(results, 100, nil, []string{"idle"})
	assert.Equal(t, 100, count, "gpu_classification filter is now handled at SQL level; filterGPUResults is a passthrough")
	assert.Len(t, out, 4)
}

func TestMatchesAny(t *testing.T) {
	assert.True(t, matchesAny("NVIDIA A100-SXM4-80GB", []string{"A100"}))
	assert.True(t, matchesAny("NVIDIA A100-SXM4-80GB", []string{"a100"}))
	assert.False(t, matchesAny("Tesla T4", []string{"A100"}))
	assert.True(t, matchesAny("Tesla T4", []string{"T4", "A100"}))
}
