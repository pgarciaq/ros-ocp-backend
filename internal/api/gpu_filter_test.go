package api

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/stretchr/testify/assert"
)

func makeTestResults() []model.NativeContainerResult {
	return []model.NativeContainerResult{
		{Container: "no-gpu", GPU: nil},
		{Container: "a100-idle", GPU: &model.GPURecommendation{
			CurrentGPUModel:   "NVIDIA A100-SXM4-80GB",
			GPUClassification: "idle",
		}},
		{Container: "t4-underutil", GPU: &model.GPURecommendation{
			CurrentGPUModel:   "Tesla T4",
			GPUClassification: "underutilized",
		}},
		{Container: "h100-well", GPU: &model.GPURecommendation{
			CurrentGPUModel:   "NVIDIA H100",
			GPUClassification: "well_utilized",
		}},
	}
}

func TestFilterGPUResults_NoFilters(t *testing.T) {
	results := makeTestResults()
	out, count := filterGPUResults(results, nil, nil, nil)
	assert.Equal(t, 4, count)
	assert.Len(t, out, 4)
}

func TestFilterGPUResults_HasGPU_True(t *testing.T) {
	results := makeTestResults()
	yes := true
	out, count := filterGPUResults(results, &yes, nil, nil)
	assert.Equal(t, 3, count)
	for _, r := range out {
		assert.NotNil(t, r.GPU)
	}
}

func TestFilterGPUResults_HasGPU_False(t *testing.T) {
	results := makeTestResults()
	no := false
	out, count := filterGPUResults(results, &no, nil, nil)
	assert.Equal(t, 1, count)
	assert.Equal(t, "no-gpu", out[0].Container)
}

func TestFilterGPUResults_GPUModel(t *testing.T) {
	results := makeTestResults()
	out, count := filterGPUResults(results, nil, []string{"A100"}, nil)
	assert.Equal(t, 1, count)
	assert.Equal(t, "a100-idle", out[0].Container)
}

func TestFilterGPUResults_GPUModel_CaseInsensitive(t *testing.T) {
	results := makeTestResults()
	out, _ := filterGPUResults(results, nil, []string{"t4"}, nil)
	assert.Len(t, out, 1)
	assert.Equal(t, "t4-underutil", out[0].Container)
}

func TestFilterGPUResults_GPUClassification(t *testing.T) {
	results := makeTestResults()
	out, count := filterGPUResults(results, nil, nil, []string{"idle"})
	assert.Equal(t, 1, count)
	assert.Equal(t, "a100-idle", out[0].Container)
}

func TestFilterGPUResults_MultipleClassifications(t *testing.T) {
	results := makeTestResults()
	_, count := filterGPUResults(results, nil, nil, []string{"idle", "underutilized"})
	assert.Equal(t, 2, count)
}

func TestFilterGPUResults_CombinedFilters(t *testing.T) {
	results := makeTestResults()
	yes := true
	out, count := filterGPUResults(results, &yes, nil, []string{"well_utilized"})
	assert.Equal(t, 1, count)
	assert.Equal(t, "h100-well", out[0].Container)
}

func TestMatchesAny(t *testing.T) {
	assert.True(t, matchesAny("NVIDIA A100-SXM4-80GB", []string{"A100"}))
	assert.True(t, matchesAny("NVIDIA A100-SXM4-80GB", []string{"a100"}))
	assert.False(t, matchesAny("Tesla T4", []string{"A100"}))
	assert.True(t, matchesAny("Tesla T4", []string{"T4", "A100"}))
}
