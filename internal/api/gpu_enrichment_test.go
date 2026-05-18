package api

import (
	"context"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestToGPURecommendation_FullData(t *testing.T) {
	savings := float32(123.45)
	rec := &engine.GPURec{
		GPUModelName:            "H100",
		CurrentGPUProfile:       "mig-3g.40gb",
		Classification:          engine.GPUClassMemoryBound,
		RecommendedGPUProfile:   "full_gpu",
		MemoryBoundDetected:     true,
		Confidence:              0.91,
		TensorPipeActiveAvg:     0.12,
		DRAMActiveAvg:           0.67,
		SMActiveAvg:             0.34,
		FBUsageMaxMiB:           8192,
		EstimatedGPUSavingsUSD:  &savings,
		NotificationCodes:       []int16{10, 20},
	}

	got := toGPURecommendation(rec)
	assert.NotNil(t, got)
	assert.Equal(t, "H100", got.CurrentGPUModel)
	assert.NotNil(t, got.CurrentGPUProfile)
	assert.Equal(t, "mig-3g.40gb", *got.CurrentGPUProfile)
	assert.Equal(t, string(engine.GPUClassMemoryBound), got.GPUClassification)
	assert.NotNil(t, got.RecommendedGPUProfile)
	assert.Equal(t, "full_gpu", *got.RecommendedGPUProfile)
	assert.True(t, got.MemoryBoundDetected)
	assert.InDelta(t, float64(0.91), float64(got.GPUConfidence), 1e-6)
	assert.InDelta(t, float64(0.12), float64(got.TensorPipeActiveAvg), 1e-6)
	assert.InDelta(t, float64(0.67), float64(got.DRAMActiveAvg), 1e-6)
	assert.InDelta(t, float64(0.34), float64(got.SMActiveAvg), 1e-6)
	assert.InDelta(t, float64(8192), float64(got.FBUsageMaxMiB), 1e-6)
	assert.NotNil(t, got.EstimatedMonthlyGPUSavingsUSD)
	assert.InDelta(t, float64(savings), float64(*got.EstimatedMonthlyGPUSavingsUSD), 1e-6)
	assert.Equal(t, []int16{10, 20}, got.Notifications)
}

func TestToGPURecommendation_NoProfiles(t *testing.T) {
	rec := &engine.GPURec{
		GPUModelName:            "A100",
		CurrentGPUProfile:       "",
		Classification:          engine.GPUClassIdle,
		RecommendedGPUProfile:   "",
		MemoryBoundDetected:     false,
		Confidence:              0.5,
		TensorPipeActiveAvg:     0,
		DRAMActiveAvg:           0,
		SMActiveAvg:             0,
		FBUsageMaxMiB:           0,
		EstimatedGPUSavingsUSD:  nil,
		NotificationCodes:       nil,
	}

	got := toGPURecommendation(rec)
	assert.NotNil(t, got)
	assert.Nil(t, got.CurrentGPUProfile)
	assert.Nil(t, got.RecommendedGPUProfile)
}

func TestToGPURecommendation_NoSavings(t *testing.T) {
	rec := &engine.GPURec{
		GPUModelName:            "L40S",
		CurrentGPUProfile:       "full",
		Classification:          engine.GPUClassWellUtilized,
		RecommendedGPUProfile:   "full",
		EstimatedGPUSavingsUSD:  nil,
		NotificationCodes:       nil,
	}

	got := toGPURecommendation(rec)
	assert.NotNil(t, got)
	assert.Nil(t, got.EstimatedMonthlyGPUSavingsUSD)
}

func TestToGPURecommendation_WithNotifications(t *testing.T) {
	rec := &engine.GPURec{
		GPUModelName:           "T4",
		Classification:         engine.GPUClassUnderutilized,
		NotificationCodes:      []int16{301, 302, 303},
		EstimatedGPUSavingsUSD: nil,
	}

	got := toGPURecommendation(rec)
	assert.NotNil(t, got)
	assert.Equal(t, []int16{301, 302, 303}, got.Notifications)
}

// --- E-T17: Container cross-reference in toGPURecommendation ---

func TestToGPURecommendation_WithTimeslicingCrossRef(t *testing.T) {
	rec := &engine.GPURec{
		GPUModelName:        "T4",
		Classification:      engine.GPUClassUnderutilized,
		SMActiveAvg:         0.12,
		Confidence:          0.8,
		NotificationCodes:   []int16{engine.NotifGPUTimeSharingCandidate},
		TimeSlicingNode:     "gpu-worker-7",
		TimeSlicingReplicas: 5,
	}

	got := toGPURecommendation(rec)
	assert.NotNil(t, got)
	assert.NotNil(t, got.TimeSlicingNode, "time_slicing_node should be set for candidates")
	assert.Equal(t, "gpu-worker-7", *got.TimeSlicingNode)
	assert.NotNil(t, got.TimeSlicingReplicas, "time_slicing_replicas should be set for candidates")
	assert.Equal(t, 5, *got.TimeSlicingReplicas)
	assert.Contains(t, got.Notifications, int16(engine.NotifGPUTimeSharingCandidate))
}

func TestToGPURecommendation_WithTimeslicingSavings(t *testing.T) {
	savings := float32(225.0)
	rec := &engine.GPURec{
		GPUModelName:                   "T4",
		Classification:                 engine.GPUClassUnderutilized,
		SMActiveAvg:                    0.12,
		Confidence:                     0.8,
		NotificationCodes:              []int16{engine.NotifGPUTimeSharingCandidate},
		TimeSlicingNode:                "gpu-worker-7",
		TimeSlicingReplicas:            4,
		EstimatedTimeslicingSavingsUSD: &savings,
	}

	got := toGPURecommendation(rec)
	assert.NotNil(t, got)
	assert.NotNil(t, got.EstimatedMonthlyTimeslicingSavingsUSD)
	assert.InDelta(t, 225.0, *got.EstimatedMonthlyTimeslicingSavingsUSD, 0.01)
}

func TestToGPURecommendation_NoTimeslicingSavings(t *testing.T) {
	rec := &engine.GPURec{
		GPUModelName:                   "T4",
		Classification:                 engine.GPUClassWellUtilized,
		SMActiveAvg:                    0.65,
		Confidence:                     0.8,
		EstimatedTimeslicingSavingsUSD: nil,
	}

	got := toGPURecommendation(rec)
	assert.NotNil(t, got)
	assert.Nil(t, got.EstimatedMonthlyTimeslicingSavingsUSD)
}

func TestToGPURecommendation_NoTimeslicingCrossRef(t *testing.T) {
	rec := &engine.GPURec{
		GPUModelName:        "T4",
		Classification:      engine.GPUClassWellUtilized,
		SMActiveAvg:         0.65,
		Confidence:          0.8,
		TimeSlicingNode:     "",
		TimeSlicingReplicas: 0,
	}

	got := toGPURecommendation(rec)
	assert.NotNil(t, got)
	assert.Nil(t, got.TimeSlicingNode, "non-candidates should have nil time_slicing_node")
	assert.Nil(t, got.TimeSlicingReplicas, "non-candidates should have nil time_slicing_replicas")
}

// --- enrichWithGPU orchestration tests ---

func TestEnrichWithGPU_EmptyResults(t *testing.T) {
	var results []model.NativeContainerResult
	enrichWithGPU(context.Background(), results, "org1234567")
}

func TestEnrichWithGPU_NilPool(t *testing.T) {
	results := []model.NativeContainerResult{
		{ClusterUUID: "cluster-1", Project: "ns", Workload: "wl", Container: "c1"},
	}
	enrichWithGPU(context.Background(), results, "org1234567")
	assert.Nil(t, results[0].GPU, "no DB pool means no GPU enrichment")
}

// --- GPUMonthlyRate (exported, shared helper) ---

func TestGPUMonthlyRate_WithValidData(t *testing.T) {
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"gpu_cost_per_month": {Infrastructure: 200.0, Supplementary: 50.0},
		},
	}
	rate := engine.GPUMonthlyRate(cd)
	assert.InDelta(t, 250.0, rate, 0.01)
}

func TestGPUMonthlyRate_NilCostData(t *testing.T) {
	rate := engine.GPUMonthlyRate(nil)
	assert.Equal(t, 0.0, rate)
}

func TestGPUMonthlyRate_MissingKey(t *testing.T) {
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_cost_per_hour": {Infrastructure: 1.0, Supplementary: 0.5},
		},
	}
	rate := engine.GPUMonthlyRate(cd)
	assert.Equal(t, 0.0, rate)
}
