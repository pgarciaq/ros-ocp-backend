package pvc

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func TestPVCPlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin      = (*PVCPlugin)(nil)
		_ plugin.CSVIngestor = (*PVCPlugin)(nil)
	)
}

func TestPVCPlugin_nameEnabledAndTypes(t *testing.T) {
	t.Parallel()

	p := &PVCPlugin{}
	assert.Equal(t, "pvc", p.Name())
	assert.True(t, p.Enabled())
	assert.Equal(t, []string{string(types.PayloadTypeStorage)}, p.SupportedCSVTypes())
}
