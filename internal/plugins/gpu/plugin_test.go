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
	assert.Equal(t, []string{
		"gpu_container_digests",
		"node_gpu_timeslicing_recommendations",
		"node_gpu_timeslicing_recommendation_history",
	}, p.RetentionTables())
}

// BH-UNIT-110: GPU plugin still implements APIEnricher for rates only.
// Dual-write of business_hours lives on ingest (gpu_stream / UpsertGPUDigests).
// Nested BH is attached in the container detail handler, not enrichWithGPU.
func TestGPUPlugin_V1_APIEnricherStaysRatesOnly(t *testing.T) {
	t.Parallel()

	p := &GPUPlugin{}
	_, isEnricher := interface{}(p).(plugin.APIEnricher)
	assert.True(t, isEnricher, "GPU plugin implements APIEnricher for rate enrichment, not BH")

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	pluginDir := filepath.Dir(thisFile)
	internalDir := filepath.Join(pluginDir, "..", "..")

	pipelineGo := filepath.Join(internalDir, "ingestion", "pipeline.go")
	body, err := os.ReadFile(pipelineGo)
	require.NoError(t, err)
	upsertBlock := extractGoFunc(string(body), "func UpsertGPUDigests")
	require.NotEmpty(t, upsertBlock)
	assert.Contains(t, upsertBlock, "ScheduleTypeBusinessHours")
	assert.Contains(t, upsertBlock, "addIfWeight")
	assert.Contains(t, upsertBlock, "ProducesBusinessHoursDigests")

	streamGo := filepath.Join(internalDir, "ingestion", "gpu_stream.go")
	streamBody, err := os.ReadFile(streamGo)
	require.NoError(t, err)
	assert.Contains(t, string(streamBody), "schedule_type")
	assert.Contains(t, string(streamBody), "addIfWeight")

	enrichGo := filepath.Join(internalDir, "api", "gpu_enrichment.go")
	enrichBody, err := os.ReadFile(enrichGo)
	require.NoError(t, err)
	assert.NotContains(t, string(enrichBody), "business_hours")
	assert.NotContains(t, string(enrichBody), "ScheduleTypeBusinessHours")
	assert.NotContains(t, string(enrichBody), "NotifGPUBHOfficeWindow")
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
