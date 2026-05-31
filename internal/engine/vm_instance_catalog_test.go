package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func TestInstanceType_ExactFit(t *testing.T) {
	match := MatchInstanceType(2, 8, vmSeriesGeneralPurpose, nil)
	require.NotNil(t, match)
	assert.Equal(t, "u1.large", match.Name)
	assert.Equal(t, vmSeriesGeneralPurpose, match.Series)
}

func TestInstanceType_UpsizeNeeded(t *testing.T) {
	match := MatchInstanceType(3, 10, vmSeriesGeneralPurpose, nil)
	require.NotNil(t, match)
	assert.Equal(t, "u1.xlarge", match.Name)
	assert.Equal(t, int32(4), match.VCPU)
	assert.Equal(t, int32(16), match.MemoryGiB)
}

func TestInstanceType_ComputeOptimizedPreferred(t *testing.T) {
	match := MatchInstanceType(4, 6, vmSeriesComputeOptimized, nil)
	require.NotNil(t, match)
	assert.Equal(t, "cx1.xlarge", match.Name)
	assert.Equal(t, vmSeriesComputeOptimized, match.Series)
}

func TestInstanceType_MemoryOptimizedPreferred(t *testing.T) {
	match := MatchInstanceType(2, 12, vmSeriesMemoryOptimized, nil)
	require.NotNil(t, match)
	assert.Equal(t, "m1.large", match.Name)
	assert.Equal(t, vmSeriesMemoryOptimized, match.Series)
}

func TestInstanceType_ExceedsCatalog(t *testing.T) {
	match := MatchInstanceType(64, 8, vmSeriesGeneralPurpose, nil)
	assert.Nil(t, match)
}

func TestInstanceType_FallbackToGeneralPurpose(t *testing.T) {
	// m-series tops out at 16 vCPU; fall back to u-series for larger CPU needs.
	match := MatchInstanceType(17, 10, vmSeriesMemoryOptimized, nil)
	require.NotNil(t, match)
	assert.Equal(t, "u1.8xlarge", match.Name)
	assert.Equal(t, vmSeriesGeneralPurpose, match.Series)
}

func TestInstanceType_TinyVM(t *testing.T) {
	match := MatchInstanceType(1, 0, vmSeriesGeneralPurpose, nil)
	require.NotNil(t, match)
	assert.Equal(t, "u1.nano", match.Name)
}

func TestInstanceType_DisabledLeavesNilInRecommender(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.EnableInstanceTypeMatching = false

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 2000
		d.CPUUsageP95MC = 500
		d.MemRequestKiB = 4 * 1024 * 1024
		d.MemUsageP95KiB = 2 * 1024 * 1024
	})

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Nil(t, rec.RecommendedInstanceType)
	assert.Nil(t, rec.RecommendedSeries)
}

func TestCatalog_ContainsNSeries(t *testing.T) {
	var found int
	for _, t := range vmInstanceCatalog {
		if t.Series == vmSeriesNetworkOptimized {
			found++
		}
	}
	assert.Equal(t, 4, found)
}

func TestCatalog_ContainsGPUSeries(t *testing.T) {
	assert.Len(t, vmInstanceCatalogGPU, 4)
	assert.Equal(t, "gn1.xlarge", vmInstanceCatalogGPU[0].Name)
}

func TestMatchInstanceType_NeverRecommendsNonSelectable(t *testing.T) {
	clusterTypes := []InstanceType{
		{Name: "n1.xlarge", Series: vmSeriesNetworkOptimized, VCPU: 4, MemoryGiB: 16, Selectable: false},
		{Name: "gn1.xlarge", Series: vmSeriesGPU, VCPU: 4, MemoryGiB: 16, GPUs: 1, Selectable: false},
	}
	match := MatchInstanceType(4, 16, vmSeriesNetworkOptimized, clusterTypes)
	require.NotNil(t, match)
	assert.NotEqual(t, "n1.xlarge", match.Name)
	assert.NotEqual(t, "gn1.xlarge", match.Name)
}

func TestMatchInstanceType_RecognizesGPUType(t *testing.T) {
	match := LookupInstanceTypeByName("gn1.xlarge", nil)
	require.NotNil(t, match)
	assert.Equal(t, "gn1.xlarge", match.Name)
	assert.Equal(t, vmSeriesGPU, match.Series)
	assert.False(t, match.Selectable)
}
