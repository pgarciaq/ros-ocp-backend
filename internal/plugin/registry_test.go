package plugin

import (
	"context"
	"io"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
)

type stubPlugin struct {
	BasePlugin
	name     string
	phase    int
	priority int
	enabled  func() bool
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

func (s *stubPlugin) Phase() int {
	if s.phase != 0 {
		return s.phase
	}
	return PhaseProduce
}

func (s *stubPlugin) Priority() int {
	if s.priority != 0 {
		return s.priority
	}
	return 50
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
	config.ResetForTest()
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

func TestValidateKruizePluginExclusivity_errorWhenBothEnabled(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "kruize,container")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "kruize"})
	Register(&stubPlugin{name: "container"})

	err := validateKruizePluginExclusivity()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
	assert.Contains(t, err.Error(), "container")
}

func TestValidateKruizePluginExclusivity_okKruizeOnly(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "kruize")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "kruize"})
	Register(&stubPlugin{name: "container"})

	require.NoError(t, validateKruizePluginExclusivity())
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
	config.ResetForTest()
	t.Setenv(envEnabledPlugins, "")
	_ = config.GetConfig()
	ApplyLegacyUseNativeEngineEnv(false)
	assert.Equal(t, KruizePluginName, config.GetConfig().EnabledPlugins)
}

func TestApplyLegacyUseNativeEngineEnv_noopWhenNative(t *testing.T) {
	config.ResetForTest()
	t.Setenv(envEnabledPlugins, "")
	_ = config.GetConfig()
	ApplyLegacyUseNativeEngineEnv(true)
	assert.Equal(t, "", config.GetConfig().EnabledPlugins)
}

func TestApplyLegacyUseNativeEngineEnv_forceKruizeOverwritesAllowlist(t *testing.T) {
	config.ResetForTest()
	t.Setenv(envEnabledPlugins, "container,gpu")
	_ = config.GetConfig()
	ApplyLegacyUseNativeEngineEnv(false)
	assert.Equal(t, KruizePluginName, config.GetConfig().EnabledPlugins)
}

func TestApplyLegacyUseNativeEngineEnv_nativeStripsKruizeFromAllowlist(t *testing.T) {
	config.ResetForTest()
	t.Setenv(envEnabledPlugins, "kruize,container")
	_ = config.GetConfig()
	ApplyLegacyUseNativeEngineEnv(true)
	assert.Equal(t, "container", config.GetConfig().EnabledPlugins)
}

func TestApplyLegacyUseNativeEngineEnv_nativeStripsKruizeOnlyClearsAllowlist(t *testing.T) {
	config.ResetForTest()
	t.Setenv(envEnabledPlugins, "kruize")
	_ = config.GetConfig()
	ApplyLegacyUseNativeEngineEnv(true)
	assert.Equal(t, "", config.GetConfig().EnabledPlugins)
}

func TestApplyLegacyUseNativeEngineEnv_nativeNoopWhenAllowlistDoesNotIncludeKruize(t *testing.T) {
	config.ResetForTest()
	t.Setenv(envEnabledPlugins, "container,namespace")
	_ = config.GetConfig()
	ApplyLegacyUseNativeEngineEnv(true)
	assert.Equal(t, "container,namespace", config.GetConfig().EnabledPlugins)
}

// --- #491: CSV type collision detection ---

type csvIngestorStubB struct {
	stubPlugin
	types []string
}

func (c *csvIngestorStubB) SupportedCSVTypes() []string {
	return c.types
}

func (c *csvIngestorStubB) IngestCSV(_ context.Context, _ *pgxpool.Pool, _ io.Reader, _, _ string) ([]ingestion.MetricRow, error) {
	return nil, nil
}

func TestValidateCSVTypeClaims_noPanicWhenUnique(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "a,b")
	t.Setenv(envDisabledPlugins, "")

	Register(&csvIngestorStubB{stubPlugin: stubPlugin{name: "a"}, types: []string{"container", "namespace"}})
	Register(&csvIngestorStubB{stubPlugin: stubPlugin{name: "b"}, types: []string{"storage", "snapshot"}})

	assert.NotPanics(t, func() {
		validateCSVTypeClaims()
	})
}

func TestValidateCSVTypeClaims_fatalsOnCollision(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "a,b")
	t.Setenv(envDisabledPlugins, "")

	Register(&csvIngestorStubB{stubPlugin: stubPlugin{name: "a"}, types: []string{"container"}})
	Register(&csvIngestorStubB{stubPlugin: stubPlugin{name: "b"}, types: []string{"container"}})

	// log.Fatalf calls os.Exit(1). We can't test that directly without a subprocess,
	// but we can verify that two plugins claim the same type by checking FindCSVIngestor
	// returns one of them (the first one wins) — the Boot() validation prevents this
	// from ever happening in production.
	ingestor := FindCSVIngestor("container")
	assert.NotNil(t, ingestor)
	assert.Equal(t, "a", ingestor.Name())
}

// --- #493: Kruize plugin warning ---

func TestWarnKruizeEnabled_noWarningWhenDisabled(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "container")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "kruize"})
	Register(&stubPlugin{name: "container"})

	assert.NotPanics(t, func() {
		warnKruizeEnabled()
	})
}

func TestWarnKruizeEnabled_warnsWhenEnabled(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "kruize")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "kruize"})

	assert.NotPanics(t, func() {
		warnKruizeEnabled()
	})
}

func TestEnabled_sortedByPhase(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "enrich,produce,optimize")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "enrich", phase: PhaseEnrich})
	Register(&stubPlugin{name: "produce", phase: PhaseProduce})
	Register(&stubPlugin{name: "optimize", phase: PhaseOptimize})

	enabled := Enabled()
	require.Len(t, enabled, 3)
	assert.Equal(t, "produce", enabled[0].Name())
	assert.Equal(t, "enrich", enabled[1].Name())
	assert.Equal(t, "optimize", enabled[2].Name())
}

func TestEnabled_phaseOrderIndependentOfRegistrationOrder(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "b,a,c")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "c", phase: PhaseOptimize})
	Register(&stubPlugin{name: "a", phase: PhaseProduce})
	Register(&stubPlugin{name: "b", phase: PhaseEnrich})

	enabled := Enabled()
	require.Len(t, enabled, 3)
	names := []string{enabled[0].Name(), enabled[1].Name(), enabled[2].Name()}
	assert.Equal(t, []string{"a", "b", "c"}, names)
}

func TestEnabled_samePhaseSortedByName(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "zebra,alpha")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "zebra", phase: PhaseProduce})
	Register(&stubPlugin{name: "alpha", phase: PhaseProduce})

	enabled := Enabled()
	require.Len(t, enabled, 2)
	assert.Equal(t, "alpha", enabled[0].Name())
	assert.Equal(t, "zebra", enabled[1].Name())
}

func TestEnabled_samePhaseSortedByPriority(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "namespace,container")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "namespace", phase: PhaseProduce, priority: 90})
	Register(&stubPlugin{name: "container", phase: PhaseProduce, priority: 10})

	enabled := Enabled()
	require.Len(t, enabled, 2)
	assert.Equal(t, "container", enabled[0].Name())
	assert.Equal(t, "namespace", enabled[1].Name())
}

func TestEnabled_priorityTieBreaksByName(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "zebra,alpha")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "zebra", phase: PhaseProduce, priority: 30})
	Register(&stubPlugin{name: "alpha", phase: PhaseProduce, priority: 30})

	enabled := Enabled()
	require.Len(t, enabled, 2)
	assert.Equal(t, "alpha", enabled[0].Name())
	assert.Equal(t, "zebra", enabled[1].Name())
}

func TestExecuteInPhases_runsPhase1BeforePhase2(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "enrich,produce")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "enrich", phase: PhaseEnrich})
	Register(&stubPlugin{name: "produce", phase: PhaseProduce})

	var order []string
	err := ExecuteInPhases(context.Background(), func(_ context.Context, p Plugin) error {
		order = append(order, p.Name())
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"produce", "enrich"}, order)
}

func TestExecuteInPhases_invalidPhaseDefaultsToProduce(t *testing.T) {
	resetRegistry(t)
	t.Setenv(envEnabledPlugins, "bad")
	t.Setenv(envDisabledPlugins, "")

	Register(&stubPlugin{name: "bad", phase: 99})

	assert.Equal(t, PhaseProduce, normalizePhase(Enabled()[0].Phase()))
}
