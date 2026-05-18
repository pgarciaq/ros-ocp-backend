package snapshot

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func TestSnapshotPlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin      = (*SnapshotPlugin)(nil)
		_ plugin.CSVIngestor = (*SnapshotPlugin)(nil)
	)
}

func TestSnapshotPlugin_nameEnabledAndTypes(t *testing.T) {
	t.Parallel()

	p := &SnapshotPlugin{}
	assert.Equal(t, "snapshot", p.Name())
	assert.True(t, p.Enabled())
	assert.Equal(t, []string{string(types.PayloadTypeSnapshot)}, p.SupportedCSVTypes())
}
