package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func digestRow(tensor, dram, sm, fbMax float32) GPUDigestRow {
	return GPUDigestRow{
		IntervalStart:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		TensorPipeActiveAvg: tensor,
		DRAMActiveAvg:       dram,
		SMActiveAvg:         sm,
		FBUsageMaxMiB:       fbMax,
	}
}

func TestClassifyGPUWorkload_Idle(t *testing.T) {
	digests := []GPUDigestRow{
		digestRow(0.1, 0.1, 0.01, 1000),
		digestRow(0.1, 0.1, 0.015, 1000),
	}
	cls, has := ClassifyGPUWorkload(digests)
	assert.True(t, has)
	assert.Equal(t, GPUClassIdle, cls)
}

func TestClassifyGPUWorkload_Underutilized(t *testing.T) {
	digests := []GPUDigestRow{
		digestRow(0.10, 0.2, 0.20, 1000),
		digestRow(0.10, 0.2, 0.22, 1000),
	}
	cls, has := ClassifyGPUWorkload(digests)
	assert.True(t, has)
	assert.Equal(t, GPUClassUnderutilized, cls)
}

func TestClassifyGPUWorkload_MemoryBound(t *testing.T) {
	digests := []GPUDigestRow{
		digestRow(0.10, 0.65, 0.50, 1000),
		digestRow(0.10, 0.66, 0.52, 1000),
	}
	cls, has := ClassifyGPUWorkload(digests)
	assert.True(t, has)
	assert.Equal(t, GPUClassMemoryBound, cls)
}

func TestClassifyGPUWorkload_WellUtilized(t *testing.T) {
	digests := []GPUDigestRow{
		digestRow(0.30, 0.20, 0.45, 1000),
		digestRow(0.30, 0.19, 0.46, 1000),
	}
	cls, has := ClassifyGPUWorkload(digests)
	assert.True(t, has)
	assert.Equal(t, GPUClassWellUtilized, cls)
}

func TestClassifyGPUWorkload_NoProfilingData(t *testing.T) {
	digests := []GPUDigestRow{
		{IntervalStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), FBUsageMaxMiB: 5000},
	}
	cls, has := ClassifyGPUWorkload(digests)
	assert.False(t, has)
	assert.Equal(t, GPUClassification(""), cls)
}

func TestGPU_MIGProfile_A100_80GB_LowFB(t *testing.T) {
	spec := gpuModels["A100_80GB"]
	digests := []GPUDigestRow{{FBUsageMaxMiB: 4500}}
	got := SelectMIGProfile(&spec, digests)
	assert.Equal(t, "1g.10gb", got)
}

func TestGPU_MIGProfile_A100_80GB_HighFB(t *testing.T) {
	// P98 FB * 1.2 must exceed largest MIG FB (81920) => use high FB max
	spec := gpuModels["A100_80GB"]
	digests := []GPUDigestRow{{FBUsageMaxMiB: 70000}}
	got := SelectMIGProfile(&spec, digests)
	assert.Equal(t, "full_gpu", got)
}

func TestGPU_MIGProfile_NonMIG(t *testing.T) {
	spec := gpuModels["T4"]
	digests := []GPUDigestRow{{FBUsageMaxMiB: 1000}}
	got := SelectMIGProfile(&spec, digests)
	assert.Equal(t, "", got)
}

func TestGPUConfidence_FewDays(t *testing.T) {
	var digests []GPUDigestRow
	for i := 0; i < 2; i++ {
		digests = append(digests, GPUDigestRow{
			SMActiveAvg: 0.5, SMActiveMax: 0.6, // not bursty
		})
	}
	c := GPUConfidence(digests)
	assert.InDelta(t, float32(0.3), c, 0.001)
}

func TestGPUConfidence_14Days(t *testing.T) {
	var digests []GPUDigestRow
	for i := 0; i < 14; i++ {
		digests = append(digests, GPUDigestRow{
			SMActiveAvg: 0.5,
			SMActiveMax: 1.5, // max/avg = 3, no penalty
		})
	}
	c := GPUConfidence(digests)
	assert.InDelta(t, float32(1.0), c, 0.001)
}

func TestGPUConfidence_Bursty(t *testing.T) {
	var digests []GPUDigestRow
	for i := 0; i < 14; i++ {
		digests = append(digests, GPUDigestRow{SMActiveAvg: 0.1, SMActiveMax: 0.11})
	}
	digests[0].SMActiveMax = 1.0 // burst: maxSM/avgSM = 10 > 5

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
			TensorPipeActiveAvg: 0.05,
			DRAMActiveAvg:       0.1,
			SMActiveAvg:         0.01,
			SMActiveMax:         0.02,
			FBUsageMaxMiB:       4096,
		})
	}
	rec := RecommendGPU(digests)
	require.NotNil(t, rec)
	assert.Equal(t, "NVIDIA A100-SXM4-80GB", rec.GPUModelName)
	assert.Equal(t, GPUClassIdle, rec.Classification)
	assert.True(t, rec.HasProfilingData)
	assert.Contains(t, rec.NotificationCodes, NotifGPUIdle)
	a100spec := gpuModels["A100_80GB"]
	exp := SelectMIGProfile(&a100spec, digests)
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
	assert.Contains(t, rec.NotificationCodes, NotifGPUNoProfilingData)
	assert.Equal(t, "", rec.RecommendedGPUProfile) // V100 is not MIG-capable in catalog
}
