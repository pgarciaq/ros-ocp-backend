package node

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func TestNodePlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin            = (*NodePlugin)(nil)
		_ plugin.IngestHook        = (*NodePlugin)(nil)
		_ plugin.APIProvider       = (*NodePlugin)(nil)
		_ plugin.RetentionProvider = (*NodePlugin)(nil)
	)
}

func TestNodePlugin_hookAfterTypes(t *testing.T) {
	t.Parallel()

	p := &NodePlugin{}
	assert.Equal(t, []string{"container"}, p.HookAfterCSVTypes())
	assert.Equal(t, []string{"daily_node_digests", "hourly_node_digests"}, p.RetentionTables())
}

// BH-UNIT-109: node plugin must not implement APIEnricher. Nested business_hours
// lives on the node detail handler. Dual-write is ingest (AddRowWeighted + schedule_type).
func TestNodePlugin_NoAPIEnricher_IngestDualWrite(t *testing.T) {
	t.Parallel()

	p := &NodePlugin{}
	_, isEnricher := interface{}(p).(plugin.APIEnricher)
	assert.False(t, isEnricher, "node plugin must not implement APIEnricher")

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	ingestionDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "ingestion")
	nodeDigest := filepath.Join(ingestionDir, "node_digest.go")
	body, err := os.ReadFile(nodeDigest)
	require.NoError(t, err)
	src := string(body)
	assert.Contains(t, src, "schedule_type")
	assert.Contains(t, src, "ScheduleTypeBusinessHours")
	assert.Contains(t, src, "AddRowWeighted")
}
