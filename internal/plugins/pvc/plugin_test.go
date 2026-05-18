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
		_ plugin.Plugin            = (*PVCPlugin)(nil)
		_ plugin.CSVIngestor       = (*PVCPlugin)(nil)
		_ plugin.APIProvider       = (*PVCPlugin)(nil)
		_ plugin.RetentionProvider = (*PVCPlugin)(nil)
	)
}

func TestPVCPlugin_nameTypesRetentionTables(t *testing.T) {
	t.Parallel()

	p := &PVCPlugin{}
	assert.Equal(t, "pvc", p.Name())
	assert.Equal(t, []string{string(types.PayloadTypeStorage)}, p.SupportedCSVTypes())
	assert.Equal(t, []string{"daily_pvc_digests"}, p.RetentionTables())
}
