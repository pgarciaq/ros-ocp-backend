package gpu

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// BH-UNIT-110: v1 GPU recommendations must not consume business_hours digest streams.
func TestGPUPlugin_V1_NoBusinessHoursStream(t *testing.T) {
	t.Parallel()

	p := &GPUPlugin{}
	_, isEnricher := interface{}(p).(plugin.APIEnricher)
	assert.True(t, isEnricher, "GPU plugin implements APIEnricher for rate enrichment, not BH")

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	pipelineGo := filepath.Join(filepath.Dir(thisFile), "..", "..", "ingestion", "pipeline.go")
	body, err := os.ReadFile(pipelineGo)
	require.NoError(t, err)
	upsertBlock := extractGoFunc(string(body), "func UpsertGPUDigests")
	require.NotEmpty(t, upsertBlock)
	assert.NotContains(t, upsertBlock, "schedule_type")
	assert.NotContains(t, upsertBlock, "business_hours")
}

func extractGoFunc(src, sigPrefix string) string {
	idx := strings.Index(src, sigPrefix)
	if idx < 0 {
		return ""
	}
	rest := src[idx:]
	depth := 0
	started := false
	for i, ch := range rest {
		if ch == '{' {
			depth++
			started = true
		} else if ch == '}' {
			depth--
			if started && depth == 0 {
				return rest[:i+1]
			}
		}
	}
	return rest
}
