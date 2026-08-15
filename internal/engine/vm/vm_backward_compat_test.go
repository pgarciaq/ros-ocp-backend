package vm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
)

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
