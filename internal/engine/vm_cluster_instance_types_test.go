package engine

import (
	"strings"
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
		{Name: "custom-db-optimized", Series: vmSeriesMemoryOptimized, VCPU: 8, MemoryGiB: 64, GPUs: 0},
	}
	match := MatchInstanceType(8, 64, vmSeriesMemoryOptimized, clusterTypes)
	require.NotNil(t, match)
	assert.Equal(t, "custom-db-optimized", match.Name)
}

func TestMatchInstanceType_EmptyClusterTypesUsesGlobal(t *testing.T) {
	match := MatchInstanceType(2, 8, vmSeriesGeneralPurpose, nil)
	require.NotNil(t, match)
	assert.Equal(t, "u1.large", match.Name)
}

func TestMatchInstanceType_ClusterFallbackToGlobal(t *testing.T) {
	clusterTypes := []InstanceType{
		{Name: "tiny", Series: vmSeriesGeneralPurpose, VCPU: 1, MemoryGiB: 1, GPUs: 0},
	}
	match := MatchInstanceType(4, 16, vmSeriesGeneralPurpose, clusterTypes)
	require.NotNil(t, match)
	assert.Equal(t, "u1.xlarge", match.Name)
}

func TestParseClusterInstanceTypesJSON(t *testing.T) {
	raw := strings.NewReader(`{
		"cluster_uuid": "550e8400-e29b-41d4-a716-446655440000",
		"collected_at": "2026-05-31T20:00:00Z",
		"instance_types": [
			{"name": "u1.large", "series": "general-purpose", "vcpu": 2, "memory_gib": 8, "gpus": 0}
		]
	}`)
	doc, err := ParseClusterInstanceTypesJSON(raw)
	require.NoError(t, err)
	require.Len(t, doc.InstanceTypes, 1)
	assert.Equal(t, "u1.large", doc.InstanceTypes[0].Name)
}

func TestIsClusterInstanceTypesFile(t *testing.T) {
	assert.True(t, IsClusterInstanceTypesFile("cluster_instance_types.json"))
	assert.True(t, IsClusterInstanceTypesFile("https://example.com/cluster_instance_types.json"))
	assert.False(t, IsClusterInstanceTypesFile("ros-openshift-vm-usage.csv"))
}
