package plugin

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
)

type stubPlugin struct {
	name    string
	enabled func() bool
}

func (s *stubPlugin) Name() string {
	return s.name
}

func (s *stubPlugin) Enabled() bool {
	if s.enabled != nil {
		return s.enabled()
	}
	return EnabledFor(s.name)
}

type csvIngestorStub struct {
	stubPlugin
}

func (c *csvIngestorStub) SupportedCSVTypes() []string {
	return []string{"container"}
}

func (c *csvIngestorStub) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	return nil, nil
}

func resetRegistry(t *testing.T) {
	t.Helper()
	regMu.Lock()
	t.Cleanup(func() {
		regMu.Lock()
		registry = nil
		regMu.Unlock()
	})
	registry = nil
	regMu.Unlock()
}

func TestRegister_nilPanics(t *testing.T) {
	resetRegistry(t)

	assert.PanicsWithValue(t, "plugin.Register: cannot register nil plugin", func() {
		Register(nil)
	})
}

func TestRegisterAndAll(t *testing.T) {
	resetRegistry(t)

	a := &stubPlugin{name: "alpha"}
	b := &stubPlugin{name: "beta"}
	Register(a)
	Register(b)

	all := All()
	require.Len(t, all, 2)
	assert.Equal(t, "alpha", all[0].Name())
	assert.Equal(t, "beta", all[1].Name())
}

func TestEnabled_allowlist(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "a,c")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "a"})
	Register(&stubPlugin{name: "b"})
	Register(&stubPlugin{name: "c"})

	enabled := Enabled()
	require.Len(t, enabled, 2)
	names := []string{enabled[0].Name(), enabled[1].Name()}
	assert.ElementsMatch(t, []string{"a", "c"}, names)
}

func TestEnabled_allowlistOverridesBlocklistConflict(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "container,gpu")
	t.Setenv(envDisabledPlugins, "gpu")

	Register(&stubPlugin{name: "container"})
	Register(&stubPlugin{name: "gpu"})
	Register(&stubPlugin{name: "node"})

	enabled := Enabled()
	require.Len(t, enabled, 2)
	names := []string{enabled[0].Name(), enabled[1].Name()}
	assert.ElementsMatch(t, []string{"container", "gpu"}, names)
}

func TestEnabled_blocklist(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "")
	t.Setenv(envDisabledPlugins, "b")

	Register(&stubPlugin{name: "a"})
	Register(&stubPlugin{name: "b"})
	Register(&stubPlugin{name: "kruize"})

	enabled := Enabled()
	require.Len(t, enabled, 1)
	assert.Equal(t, "a", enabled[0].Name())
}

func TestEnabled_kruizeDefaultOff(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "kruize"})
	Register(&stubPlugin{name: "container"})

	enabled := Enabled()
	require.Len(t, enabled, 1)
	assert.Equal(t, "container", enabled[0].Name())
}

func TestEnabled_kruizeExclusivity(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "kruize,container")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "kruize"})
	Register(&stubPlugin{name: "container"})

	enabled := Enabled()
	require.Len(t, enabled, 1)
	assert.Equal(t, "kruize", enabled[0].Name())
}

func TestEnabled_kruizeAllowlistOnly(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "kruize")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "kruize"})

	enabled := Enabled()
	require.Len(t, enabled, 1)
	assert.Equal(t, "kruize", enabled[0].Name())
}

func TestByTrait_CSVIngestor(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "ingest-one,other")
	t.Setenv(envDisabledPlugins, "")

	Register(&csvIngestorStub{stubPlugin: stubPlugin{name: "ingest-one"}})
	Register(&stubPlugin{name: "other"})

	found := ByTrait[CSVIngestor]()
	require.Len(t, found, 1)
	assert.Equal(t, "ingest-one", found[0].Name())
}

func TestApplyLegacyUseNativeEngineEnv_setsKruizeWhenUnset(t *testing.T) {
	t.Setenv(envEnabledPlugins, "")
	ApplyLegacyUseNativeEngineEnv(false)
	assert.Equal(t, KruizePluginName, os.Getenv(envEnabledPlugins))
}

func TestApplyLegacyUseNativeEngineEnv_noopWhenNative(t *testing.T) {
	t.Setenv(envEnabledPlugins, "")
	ApplyLegacyUseNativeEngineEnv(true)
	assert.Equal(t, "", os.Getenv(envEnabledPlugins))
}

func TestApplyLegacyUseNativeEngineEnv_forceKruizeOverwritesAllowlist(t *testing.T) {
	t.Setenv(envEnabledPlugins, "container,gpu")
	ApplyLegacyUseNativeEngineEnv(false)
	assert.Equal(t, KruizePluginName, os.Getenv(envEnabledPlugins))
}

func TestApplyLegacyUseNativeEngineEnv_nativeStripsKruizeFromAllowlist(t *testing.T) {
	t.Setenv(envEnabledPlugins, "kruize,container")
	ApplyLegacyUseNativeEngineEnv(true)
	assert.Equal(t, "container", os.Getenv(envEnabledPlugins))
}

func TestApplyLegacyUseNativeEngineEnv_nativeStripsKruizeOnlyClearsAllowlist(t *testing.T) {
	t.Setenv(envEnabledPlugins, "kruize")
	ApplyLegacyUseNativeEngineEnv(true)
	assert.Equal(t, "", os.Getenv(envEnabledPlugins))
}

func TestApplyLegacyUseNativeEngineEnv_nativeNoopWhenAllowlistDoesNotIncludeKruize(t *testing.T) {
	t.Setenv(envEnabledPlugins, "container,namespace")
	ApplyLegacyUseNativeEngineEnv(true)
	assert.Equal(t, "container,namespace", os.Getenv(envEnabledPlugins))
}
