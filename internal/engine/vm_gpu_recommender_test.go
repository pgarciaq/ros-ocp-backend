package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func vmGPUDigests(mutate func(*model.DailyVMDigest)) []model.DailyVMDigest {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	return vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.HasGPU = true
		d.GPUCount = 1
		d.GPUModel = "NVIDIA A100-SXM4-80GB"
		if mutate != nil {
			mutate(d)
		}
	})
}

func TestVMGPU_IdleClassification(t *testing.T) {
	digests := vmGPUDigests(func(d *model.DailyVMDigest) {
		d.GPUSMActiveAvgBP = 100
		d.GPUTensorAvgBP = 50
		d.GPUUtilAvgBP = 200
	})
	cfg := DefaultVMRecConfig()
	analysis := analyzeVMGPU(digests, cfg)
	assert.Equal(t, "idle", analysis.Classification)
	assert.Equal(t, vmGPUActionRemoveGPU, analysis.Action)
	assert.Contains(t, analysis.NotificationCodes, NotifVMGPUIdle)
}

func TestVMGPU_UnderutilizedPassthrough(t *testing.T) {
	digests := vmGPUDigests(func(d *model.DailyVMDigest) {
		d.GPUSMActiveAvgBP = 1500
		d.GPUTensorAvgBP = 800
		d.GPUUtilAvgBP = 1200
		d.GPUMIGProfile = ""
	})
	cfg := DefaultVMRecConfig()
	analysis := analyzeVMGPU(digests, cfg)
	assert.Equal(t, "underutilized", analysis.Classification)
	assert.Equal(t, vmGPUActionConsiderVGPUOrMIG, analysis.Action)
	assert.Contains(t, analysis.NotificationCodes, NotifVMGPUUnderutilized)
}

func TestVMGPU_UnderutilizedMIG(t *testing.T) {
	digests := vmGPUDigests(func(d *model.DailyVMDigest) {
		d.GPUSMActiveAvgBP = 1200
		d.GPUTensorAvgBP = 600
		d.GPUMIGProfile = "3g.20gb"
	})
	cfg := DefaultVMRecConfig()
	analysis := analyzeVMGPU(digests, cfg)
	assert.Equal(t, "underutilized", analysis.Classification)
	assert.Equal(t, vmGPUActionSmallerMIGProfile, analysis.Action)
	assert.NotEmpty(t, analysis.Profile)
	assert.Contains(t, analysis.NotificationCodes, NotifVMGPUUnderutilized)
}

func TestVMGPU_MemorySaturated(t *testing.T) {
	digests := vmGPUDigests(func(d *model.DailyVMDigest) {
		d.GPUFBUsedMaxMiB = 75000
		d.GPUSMActiveAvgBP = 5000
		d.GPUTensorAvgBP = 4000
	})
	cfg := DefaultVMRecConfig()
	analysis := analyzeVMGPU(digests, cfg)
	assert.Equal(t, "memory_saturated", analysis.Classification)
	assert.Equal(t, vmGPUActionLargerGPU, analysis.Action)
	assert.Contains(t, analysis.NotificationCodes, NotifVMGPUMemorySaturated)
}

func TestVMGPU_ComputeSaturated(t *testing.T) {
	digests := vmGPUDigests(func(d *model.DailyVMDigest) {
		d.GPUUtilAvgBP = 9000
		d.GPUSMActiveAvgBP = 5000
		d.GPUTensorAvgBP = 4000
		d.GPUFBUsedMaxMiB = 10000
	})
	cfg := DefaultVMRecConfig()
	analysis := analyzeVMGPU(digests, cfg)
	assert.Equal(t, "compute_saturated", analysis.Classification)
	assert.Equal(t, vmGPUActionMorePowerfulGPU, analysis.Action)
	assert.Contains(t, analysis.NotificationCodes, NotifVMGPUComputeSaturated)
}

func TestVMGPU_WellUtilized(t *testing.T) {
	digests := vmGPUDigests(func(d *model.DailyVMDigest) {
		d.GPUUtilAvgBP = 5000
		d.GPUSMActiveAvgBP = 4000
		d.GPUTensorAvgBP = 3500
		d.GPUFBUsedMaxMiB = 20000
	})
	cfg := DefaultVMRecConfig()
	analysis := analyzeVMGPU(digests, cfg)
	assert.Equal(t, "well_utilized", analysis.Classification)
	assert.Equal(t, vmGPUActionNoChange, analysis.Action)
	assert.Empty(t, analysis.NotificationCodes)
}

func TestVMGPU_NoGPU_SkipsAnalysis(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, nil)
	cfg := DefaultVMRecConfig()
	analysis := analyzeVMGPU(digests, cfg)
	assert.Empty(t, analysis.Classification)
	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, int32(0), rec.GPUCount)
	assert.Empty(t, rec.GPUClassification)
}

func TestInstanceType_GPUMatching(t *testing.T) {
	match := MatchInstanceType(8, 32, vmSeriesGPU, nil, true, 1, 16)
	require.NotNil(t, match)
	assert.Equal(t, "gn1.2xlarge", match.Name)
	assert.Equal(t, int32(1), match.GPUs)
}

func TestInstanceType_GPUMemoryFit(t *testing.T) {
	match := MatchInstanceType(16, 64, vmSeriesGPU, nil, true, 1, 50)
	require.NotNil(t, match)
	assert.GreaterOrEqual(t, match.GPUMemoryGiB, int32(50))
}

func TestInstanceType_NoGPU_ExcludesGN1(t *testing.T) {
	match := MatchInstanceType(4, 16, vmSeriesGeneralPurpose, nil, false, 0, 0)
	require.NotNil(t, match)
	assert.NotEqual(t, vmSeriesGPU, match.Series)
}
