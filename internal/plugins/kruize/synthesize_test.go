package kruize

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func synthTestTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func synthP64(v int64) *int64 { return &v }

// Full six-row input: amounts hand-verifiable (millicores→cores,
// KiB→bytes), variation as absolute deltas for the reader recompute path.
func synthFullRows() []SynthInput {
	startS := synthTestTime("2024-01-14T00:00:00Z")
	endS := synthTestTime("2024-01-15T00:00:00Z")
	startM := synthTestTime("2024-01-08T00:00:00Z")
	startL := synthTestTime("2023-12-31T00:00:00Z")
	mk := func(term, engine string, start time.Time, recReqCPU, recReqMem, recLimCPU, recLimMem int64) SynthInput {
		return SynthInput{
			Term: term, Engine: engine,
			CurrentCPURequestMC: synthP64(1000), CurrentMemRequestKiB: synthP64(1048576),
			CurrentCPULimitMC: synthP64(2000), CurrentMemLimitKiB: synthP64(2097152),
			RecCPURequestMC: synthP64(recReqCPU), RecMemRequestKiB: synthP64(recReqMem),
			RecCPULimitMC: synthP64(recLimCPU), RecMemLimitKiB: synthP64(recLimMem),
			MonitoringStartTime: start, MonitoringEndTime: endS,
		}
	}
	return []SynthInput{
		mk("short_term", "cost", startS, 500, 524288, 1500, 1572864),
		mk("short_term", "performance", startS, 2000, 2097152, 3000, 3145728),
		mk("medium_term", "cost", startM, 400, 419430, 1200, 1258291),
		mk("medium_term", "performance", startM, 1500, 1572864, 2500, 2621440),
		mk("long_term", "cost", startL, 300, 314572, 1000, 1048576),
		mk("long_term", "performance", startL, 1200, 1258291, 2000, 2097152),
	}
}

func synthAmount(t *testing.T, obj map[string]interface{}, key, format string, want float64) {
	t.Helper()
	v, ok := obj[key].(map[string]interface{})
	require.True(t, ok, "missing %q", key)
	assert.InDelta(t, want, v["amount"], 1e-9, "%s amount", key)
	assert.Equal(t, format, v["format"], "%s format", key)
}

// TestSynthesizeKruizeJSON_FullShape pins the legacy blob contract:
// base units cores/bytes, absolute-delta variation (reader recomputes
// exact pcts), millis-Z times, durations, all 3 terms × 2 engines.
func TestSynthesizeKruizeJSON_FullShape(t *testing.T) {
	data := SynthesizeKruizeJSON(synthFullRows())

	assert.Equal(t, "2024-01-15T00:00:00.000Z", data["monitoring_end_time"])

	req := data["current"].(map[string]interface{})["requests"].(map[string]interface{})
	synthAmount(t, req, "cpu", "cores", 1.0)
	synthAmount(t, req, "memory", "bytes", 1073741824.0)
	lim := data["current"].(map[string]interface{})["limits"].(map[string]interface{})
	synthAmount(t, lim, "cpu", "cores", 2.0)
	synthAmount(t, lim, "memory", "bytes", 2147483648.0)

	terms := data["recommendation_terms"].(map[string]interface{})
	require.Len(t, terms, 3)
	short := terms["short_term"].(map[string]interface{})
	assert.Equal(t, 24.0, short["duration_in_hours"])
	assert.Equal(t, "2024-01-14T00:00:00.000Z", short["monitoring_start_time"])
	medium := terms["medium_term"].(map[string]interface{})
	assert.Equal(t, 168.0, medium["duration_in_hours"])
	longT := terms["long_term"].(map[string]interface{})
	assert.Equal(t, 360.0, longT["duration_in_hours"])

	cost := short["recommendation_engines"].(map[string]interface{})["cost"].(map[string]interface{})
	cfg := cost["config"].(map[string]interface{})
	synthAmount(t, cfg["requests"].(map[string]interface{}), "cpu", "cores", 0.5)
	synthAmount(t, cfg["requests"].(map[string]interface{}), "memory", "bytes", 536870912.0)
	variation := cost["variation"].(map[string]interface{})
	synthAmount(t, variation["requests"].(map[string]interface{}), "cpu", "cores", -0.5)
	synthAmount(t, variation["requests"].(map[string]interface{}), "memory", "bytes", -536870912.0)
	synthAmount(t, variation["limits"].(map[string]interface{}), "cpu", "cores", -0.5)

	perf := short["recommendation_engines"].(map[string]interface{})["performance"].(map[string]interface{})
	perfCfg := perf["config"].(map[string]interface{})
	synthAmount(t, perfCfg["requests"].(map[string]interface{}), "cpu", "cores", 2.0)

	// No plots, no notifications in Phase 1 output.
	_, hasPlots := short["plots"]
	assert.False(t, hasPlots)
	_, hasNotif := cost["notifications"]
	assert.False(t, hasNotif)
}

// TestSynthesizeKruizeJSON_PartialInput omits missing terms/engines and
// nil limit sections instead of fabricating them.
func TestSynthesizeKruizeJSON_PartialInput(t *testing.T) {
	rows := []SynthInput{synthFullRows()[0]} // short/cost only, strip limits
	rows[0].RecCPULimitMC = nil
	rows[0].RecMemLimitKiB = nil
	rows[0].CurrentCPULimitMC = nil
	rows[0].CurrentMemLimitKiB = nil

	data := SynthesizeKruizeJSON(rows)
	terms := data["recommendation_terms"].(map[string]interface{})
	require.Len(t, terms, 1)
	short := terms["short_term"].(map[string]interface{})
	engines := short["recommendation_engines"].(map[string]interface{})
	require.Len(t, engines, 1)
	cost := engines["cost"].(map[string]interface{})
	cfg := cost["config"].(map[string]interface{})
	_, hasLimits := cfg["limits"]
	assert.False(t, hasLimits, "nil limits must omit the section")
	_, hasReq := cfg["requests"]
	assert.True(t, hasReq, "present requests must render")
}
