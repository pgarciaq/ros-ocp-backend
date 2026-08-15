package vm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseClusterInstanceTypesJSON_WithPreferences(t *testing.T) {
	raw := strings.NewReader(`{
		"cluster_uuid": "550e8400-e29b-41d4-a716-446655440000",
		"collected_at": "2026-05-31T20:00:00Z",
		"instance_types": [{"name": "u1.large", "series": "general-purpose", "vcpu": 2, "memory_gib": 8}],
		"preferences": [{"name": "database", "class": "memory-intensive"}],
		"vm_preferences": {"production/db-server-01": "database"}
	}`)
	doc, err := ParseClusterInstanceTypesJSON(raw)
	require.NoError(t, err)
	require.Len(t, doc.Preferences, 1)
	assert.Equal(t, "database", doc.Preferences[0].Name)
	assert.Equal(t, "database", doc.VMPreferences["production/db-server-01"])
}

func TestParseClusterInstanceTypesJSON_NoPreferences(t *testing.T) {
	raw := strings.NewReader(`{
		"cluster_uuid": "550e8400-e29b-41d4-a716-446655440000",
		"instance_types": [{"name": "u1.large", "series": "general-purpose", "vcpu": 2, "memory_gib": 8}]
	}`)
	doc, err := ParseClusterInstanceTypesJSON(raw)
	require.NoError(t, err)
	assert.Empty(t, doc.Preferences)
	assert.Nil(t, doc.VMPreferences)
}
