package gpu

import (
	"math"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bp(v float32) int32 { return FloatToBasisPoints(float64(v)) }

func digestRow(tensor, dram, sm, fbMax float32) GPUDigestRow {
	return GPUDigestRow{
		IntervalStart:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		TensorPipeActiveAvg: FloatToBasisPoints(float64(tensor)),
		DRAMActiveAvg:       FloatToBasisPoints(float64(dram)),
		SMActiveAvg:         FloatToBasisPoints(float64(sm)),
		FBUsageMaxMiB:       int32(math.Round(float64(fbMax))),
	}
}

func TestClassifyGPUWorkload_Idle(t *testing.T) {
	th := DefaultGPUThresholds()
	digests := []GPUDigestRow{
		digestRow(0.1, 0.1, 0.01, 1000),
		digestRow(0.1, 0.1, 0.015, 1000),
	}
	cls, has := th.Classify(digests)
	assert.True(t, has)
	assert.Equal(t, GPUClassIdle, cls)
}

func TestClassifyGPUWorkload_Underutilized(t *testing.T) {
	th := DefaultGPUThresholds()
	digests := []GPUDigestRow{
		digestRow(0.10, 0.2, 0.20, 1000),
		digestRow(0.10, 0.2, 0.22, 1000),
	}
	cls, has := th.Classify(digests)
	assert.True(t, has)
	assert.Equal(t, GPUClassUnderutilized, cls)
}

func TestClassifyGPUWorkload_MemoryBound(t *testing.T) {
	th := DefaultGPUThresholds()
	digests := []GPUDigestRow{
		digestRow(0.10, 0.65, 0.50, 1000),
		digestRow(0.10, 0.66, 0.52, 1000),
	}
	cls, has := th.Classify(digests)
	assert.True(t, has)
	assert.Equal(t, GPUClassMemoryBound, cls)
}

func TestClassifyGPUWorkload_WellUtilized(t *testing.T) {
	th := DefaultGPUThresholds()
	digests := []GPUDigestRow{
		digestRow(0.30, 0.20, 0.45, 1000),
		digestRow(0.30, 0.19, 0.46, 1000),
	}
	cls, has := th.Classify(digests)
	assert.True(t, has)
	assert.Equal(t, GPUClassWellUtilized, cls)
}

func TestClassifyGPUWorkload_NoProfilingData(t *testing.T) {
	th := DefaultGPUThresholds()
	digests := []GPUDigestRow{
		{IntervalStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), FBUsageMaxMiB: 5000},
	}
	cls, has := th.Classify(digests)
	assert.False(t, has)
	assert.Equal(t, GPUClassification(""), cls)
}

func TestGPU_MIGProfile_A100_80GB_LowFB(t *testing.T) {
	th := DefaultGPUThresholds()
	spec := gpuModels["A100_80GB"]
	digests := []GPUDigestRow{{FBUsageMaxMiB: 4500}}
	got := th.SelectMIGProfile(&spec, digests)
	assert.Equal(t, "1g.10gb", got)
}

func TestGPU_MIGProfile_A100_80GB_HighFB(t *testing.T) {
	th := DefaultGPUThresholds()
	spec := gpuModels["A100_80GB"]
	digests := []GPUDigestRow{{FBUsageMaxMiB: 70000}}
	got := th.SelectMIGProfile(&spec, digests)
	assert.Equal(t, "full_gpu", got)
}

func TestGPU_MIGProfile_NonMIG(t *testing.T) {
	th := DefaultGPUThresholds()
	spec := gpuModels["T4"]
	digests := []GPUDigestRow{{FBUsageMaxMiB: 1000}}
	got := th.SelectMIGProfile(&spec, digests)
	assert.Equal(t, "", got)
}

func TestGPUConfidence_FewDays(t *testing.T) {
	var digests []GPUDigestRow
	for i := 0; i < 2; i++ {
		digests = append(digests, GPUDigestRow{
			SMActiveAvg: bp(0.5), SMActiveMax: bp(0.6),
		})
	}
	c := GPUConfidence(digests)
	assert.InDelta(t, float32(0.3), c, 0.001)
}

func TestGPUConfidence_14Days(t *testing.T) {
	var digests []GPUDigestRow
	for i := 0; i < 14; i++ {
		digests = append(digests, GPUDigestRow{
			SMActiveAvg: bp(0.5),
			SMActiveMax: bp(1.5),
		})
	}
	c := GPUConfidence(digests)
	assert.InDelta(t, float32(1.0), c, 0.001)
}

func TestGPUConfidence_Bursty(t *testing.T) {
	var digests []GPUDigestRow
	for i := 0; i < 14; i++ {
		digests = append(digests, GPUDigestRow{SMActiveAvg: bp(0.1), SMActiveMax: bp(0.11)})
	}
	digests[0].SMActiveMax = bp(1.0)

	c := GPUConfidence(digests)
	assert.InDelta(t, float32(0.7), c, 0.001)
}

func TestRecommendGPU_IdleA100(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var digests []GPUDigestRow
	for i := 0; i < 5; i++ {
		digests = append(digests, GPUDigestRow{
			IntervalStart:       base.AddDate(0, 0, i),
			GPUModelName:        "NVIDIA A100-SXM4-80GB",
			GPUProfileName:      "",
			TensorPipeActiveAvg: bp(0.05),
			DRAMActiveAvg:       bp(0.1),
			SMActiveAvg:         bp(0.01),
			SMActiveMax:         bp(0.02),
			FBUsageMaxMiB:       4096,
		})
	}
	rec := RecommendGPU(digests)
	require.NotNil(t, rec)
	assert.Equal(t, "NVIDIA A100-SXM4-80GB", rec.GPUModelName)
	assert.Equal(t, GPUClassIdle, rec.Classification)
	assert.True(t, rec.HasProfilingData)
	assert.Contains(t, rec.NotificationCodes, core.NotifGPUIdle)
	th := DefaultGPUThresholds()
	a100spec := gpuModels["A100_80GB"]
	exp := th.SelectMIGProfile(&a100spec, digests)
	assert.Equal(t, exp, rec.RecommendedGPUProfile)
}

func TestRecommendGPU_NoGPUData(t *testing.T) {
	assert.Nil(t, RecommendGPU(nil))
	assert.Nil(t, RecommendGPU([]GPUDigestRow{}))
}

func TestRecommendGPU_Tier2_V100(t *testing.T) {
	digests := []GPUDigestRow{
		{
			IntervalStart:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			GPUModelName:   "Tesla V100-SXM2-32GB",
			GPUProfileName: "",
			FBUsageMaxMiB:  8000,
			FBUsageAvgMiB:  7000,
		},
	}
	rec := RecommendGPU(digests)
	require.NotNil(t, rec)
	assert.False(t, rec.HasProfilingData)
	assert.Equal(t, GPUClassNoProfiling, rec.Classification)
	assert.Contains(t, rec.NotificationCodes, core.NotifGPUNoProfilingData)
	assert.Equal(t, "", rec.RecommendedGPUProfile)
}

// --- GPUThresholds struct tests (parallel-safe, no global mutation) ---

func TestGPUThresholdsFromConfig(t *testing.T) {
	cfg := &config.Config{
		GPUIdleThreshold:                0.05,
		GPUUnderutilizedSMThreshold:     0.99,
		GPUUnderutilizedTensorThreshold: 0.20,
		GPUMemBoundDRAMThreshold:        0.70,
		GPUMemBoundTensorThreshold:      0.18,
		GPUFBHeadroomFactor:             1.30,
	}
	th := GPUThresholdsFromConfig(cfg)
	assert.InDelta(t, 0.05, th.IdleThreshold, 1e-9)
	assert.InDelta(t, 0.99, th.UnderutilizedSM, 1e-9)
	assert.InDelta(t, 0.20, th.UnderutilizedTensor, 1e-9)
	assert.InDelta(t, 0.70, th.MemBoundDRAM, 1e-9)
	assert.InDelta(t, 0.18, th.MemBoundTensor, 1e-9)
	assert.InDelta(t, 1.30, th.FBHeadroomFactor, 1e-9)
}

func TestGPUThresholdsFromConfig_Nil(t *testing.T) {
	th := GPUThresholdsFromConfig(nil)
	defaults := DefaultGPUThresholds()
	assert.Equal(t, defaults, th)
}


func TestClassify_IdleThresholdOverride(t *testing.T) {
	digests := []GPUDigestRow{
		digestRow(0.1, 0.1, 0.03, 1000),
		digestRow(0.1, 0.1, 0.03, 1000),
	}

	defaults := DefaultGPUThresholds()
	cls, hasProf := defaults.Classify(digests)
	assert.True(t, hasProf)
	assert.NotEqual(t, GPUClassIdle, cls, "0.03 SM should not be idle with default threshold 0.02")

	raised := GPUThresholds{
		IdleThreshold:       0.05,
		UnderutilizedSM:     defaults.UnderutilizedSM,
		UnderutilizedTensor: defaults.UnderutilizedTensor,
		MemBoundDRAM:        defaults.MemBoundDRAM,
		MemBoundTensor:      defaults.MemBoundTensor,
		FBHeadroomFactor:    defaults.FBHeadroomFactor,
	}
	normalizeGPUThresholds(&raised)
	cls2, hasProf2 := raised.Classify(digests)
	assert.True(t, hasProf2)
	assert.Equal(t, GPUClassIdle, cls2, "0.03 SM should be idle with raised threshold 0.05")
}

func TestClassify_MemBoundThresholdOverride(t *testing.T) {
	digests := []GPUDigestRow{
		digestRow(0.10, 0.55, 0.30, 1000),
	}

	defaults := DefaultGPUThresholds()
	cls, _ := defaults.Classify(digests)
	assert.NotEqual(t, GPUClassMemoryBound, cls, "should NOT be memory_bound with default DRAM threshold 0.60")

	lowered := GPUThresholds{
		IdleThreshold:       defaults.IdleThreshold,
		UnderutilizedSM:     defaults.UnderutilizedSM,
		UnderutilizedTensor: defaults.UnderutilizedTensor,
		MemBoundDRAM:        0.50,
		MemBoundTensor:      defaults.MemBoundTensor,
		FBHeadroomFactor:    defaults.FBHeadroomFactor,
	}
	normalizeGPUThresholds(&lowered)
	cls2, _ := lowered.Classify(digests)
	assert.Equal(t, GPUClassMemoryBound, cls2, "should be memory_bound with lowered DRAM threshold 0.50")
}

func TestFilterGPUByWindow(t *testing.T) {
	rows := []GPUDigestRow{
		{IntervalStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), SMActiveAvg: bp(0.1)},
		{IntervalStart: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), SMActiveAvg: bp(0.2)},
		{IntervalStart: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), SMActiveAvg: bp(0.3)},
		{IntervalStart: time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC), SMActiveAvg: bp(0.4)},
		{IntervalStart: time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), SMActiveAvg: bp(0.5)},
	}

	endDate := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)

	result := filterGPUByWindow(rows, endDate, 1)
	require.Len(t, result, 1)
	assert.Equal(t, 5, result[0].IntervalStart.Day())

	result = filterGPUByWindow(rows, endDate, 3)
	require.Len(t, result, 3)

	result = filterGPUByWindow(rows, endDate, 30)
	require.Len(t, result, 5)
}

func TestLatestGPUDigest(t *testing.T) {
	rows := []GPUDigestRow{
		{IntervalStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)},
		{IntervalStart: time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)},
		{IntervalStart: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)},
	}
	latest := latestGPUDigest(rows)
	assert.Equal(t, 5, latest.IntervalStart.Day())
}

func TestLatestGPUDigest_Empty(t *testing.T) {
	latest := latestGPUDigest(nil)
	assert.True(t, latest.IntervalStart.IsZero())
}
