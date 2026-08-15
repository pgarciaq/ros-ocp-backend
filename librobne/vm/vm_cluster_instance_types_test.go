package vm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeInstanceTypeSeries(t *testing.T) {
	assert.Equal(t, vmSeriesComputeOptimized, NormalizeInstanceTypeSeries("compute-intensive"))
	assert.Equal(t, vmSeriesMemoryOptimized, NormalizeInstanceTypeSeries("memory-intensive"))
	assert.Equal(t, vmSeriesGeneralPurpose, NormalizeInstanceTypeSeries("general-purpose"))
	assert.Equal(t, vmSeriesGeneralPurpose, NormalizeInstanceTypeSeries(""))
}

func TestMatchInstanceType_ClusterCatalogOverridesGlobal(t *testing.T) {
	clusterTypes := []InstanceType{
		{Name: "custom-db-optimized", Series: vmSeriesMemoryOptimized, VCPU: 8, MemoryGiB: 64, GPUs: 0, Selectable: true},
	}
	match := MatchInstanceType(8, 64, vmSeriesMemoryOptimized, clusterTypes, false, 0, 0, true)
	require.NotNil(t, match)
	assert.Equal(t, "custom-db-optimized", match.Name)
}

func TestMatchInstanceType_EmptyClusterTypesUsesGlobal(t *testing.T) {
	match := MatchInstanceType(2, 8, vmSeriesGeneralPurpose, nil, false, 0, 0, true)
	require.NotNil(t, match)
	assert.Equal(t, "u1.large", match.Name)
}

func TestMatchInstanceType_ClusterFallbackToGlobal(t *testing.T) {
	clusterTypes := []InstanceType{
		{Name: "tiny", Series: vmSeriesGeneralPurpose, VCPU: 1, MemoryGiB: 1, GPUs: 0},
	}
	match := MatchInstanceType(4, 16, vmSeriesGeneralPurpose, clusterTypes, false, 0, 0, true)
	require.NotNil(t, match)
	assert.Equal(t, "u1.xlarge", match.Name)
}
