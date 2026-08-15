package vm

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
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

// TestVMCSVParse_OldFormatWithoutRestartCount parses legacy VM usage CSV without restart_count.
func TestVMCSVParse_OldFormatWithoutRestartCount(t *testing.T) {
	csv := ingestion.CanonicalVMUsageCSVHeader() + `
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,legacy-vm,apps,node-a,linux,500,1000,2000,524288,1048576,1572864,10737418240,53687091200,107374182400,120,80,1048576,524288
`
	rows, err := ingestion.ParseVMCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Nil(t, rows[0].RestartCount)
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
