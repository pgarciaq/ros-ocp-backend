package vm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVMRecommend_NoGPUDeviceData verifies recommendations run when GPU device rows are absent.
func TestVMRecommend_NoGPUDeviceData(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, nil)
	for i := range digests {
		digests[i].Devices = nil
		digests[i].HasGPU = false
		digests[i].GPUCount = 0
	}

	cfg := DefaultVMRecConfig()
	analysis := analyzeVMGPU(digests, cfg)
	assert.Empty(t, analysis.Classification)
	assert.Empty(t, analysis.GPUDevices)

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, int32(0), rec.GPUCount)
	assert.Empty(t, rec.GPUClassification)
}

// TestVMRecommend_NoClusterInstanceTypes uses static catalog when cluster types are empty.
func TestVMRecommend_NoClusterInstanceTypes(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *Digest) {
		d.CPUUsageP95MC = 500
		d.CPUUsageP99MC = 600
		d.CPULimitMC = 8000
		d.CPURequestMC = 4000
	})
	cfg := DefaultVMRecConfig()
	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.RecommendedInstanceType)
	assert.NotEmpty(t, *rec.RecommendedInstanceType)
}

// TestVMRecommend_NoPreferences documents nil preference fields when catalog is absent.
func TestVMRecommend_NoPreferences(t *testing.T) {
	ctx := (*VMPreferenceContext)(nil)
	name, class := ctx.PreferenceInfoForVM("production", "any-vm")
	assert.Empty(t, name)
	assert.Empty(t, class)

	empty := buildVMPreferenceContext(nil, nil)
	assert.Nil(t, empty)
	name, class = empty.PreferenceInfoForVM("production", "any-vm")
	assert.Empty(t, name)
	assert.Empty(t, class)
}
