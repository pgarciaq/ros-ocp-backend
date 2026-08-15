package vm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
