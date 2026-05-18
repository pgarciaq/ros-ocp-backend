package gpu

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func TestGPUPlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin            = (*GPUPlugin)(nil)
		_ plugin.IngestHook        = (*GPUPlugin)(nil)
		_ plugin.APIProvider       = (*GPUPlugin)(nil)
		_ plugin.APIEnricher       = (*GPUPlugin)(nil)
		_ plugin.RetentionProvider = (*GPUPlugin)(nil)
	)
}

func TestGPUPlugin_hookAfterTypes(t *testing.T) {
	t.Parallel()

	p := &GPUPlugin{}
	assert.Equal(t, []string{"container"}, p.HookAfterCSVTypes())
	assert.Equal(t, []string{"gpu_container_digests"}, p.RetentionTables())
}
