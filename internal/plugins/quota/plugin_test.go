package quota

import (
	"context"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func syncPluginConfigFromEnv(t *testing.T) {
	t.Helper()
	config.ResetForTest()
	_ = config.GetConfig()
}

func TestQuotaPlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin            = (*QuotaPlugin)(nil)
		_ plugin.IngestHook        = (*QuotaPlugin)(nil)
		_ plugin.APIProvider       = (*QuotaPlugin)(nil)
		_ plugin.RetentionProvider = (*QuotaPlugin)(nil)
	)
}

func TestQuotaPlugin_Metadata(t *testing.T) {
	t.Parallel()

	p := &QuotaPlugin{}
	assert.Equal(t, "quota", p.Name())
	assert.Equal(t, plugin.PhaseProduce, p.Phase())
	assert.Equal(t, 35, p.Priority())
	assert.Equal(t, []string{"namespace"}, p.HookAfterCSVTypes())
	assert.Equal(t, []string{"quota_recommendation_sets"}, p.RetentionTables())
}

func TestQuotaPlugin_EnabledByDefault(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	syncPluginConfigFromEnv(t)

	p := &QuotaPlugin{}
	assert.True(t, p.Enabled())
	assert.True(t, plugin.EnabledFor("quota"))
}

func TestQuotaPlugin_DisabledViaEnv(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "quota")
	syncPluginConfigFromEnv(t)

	p := &QuotaPlugin{}
	assert.False(t, p.Enabled())
	assert.False(t, plugin.EnabledFor("quota"))
}

func TestQuotaPlugin_AllowlistRequiresQuota(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	syncPluginConfigFromEnv(t)

	p := &QuotaPlugin{}
	assert.False(t, p.Enabled())
}

func TestQuotaPlugin_RegisterRoutes_SkipsWhenKruizeEnabled(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "kruize")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	syncPluginConfigFromEnv(t)

	p := &QuotaPlugin{}
	require.True(t, plugin.EnabledFor(plugin.KruizePluginName))

	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	before := len(e.Routes())
	p.RegisterRoutes(v1)
	assert.Equal(t, before, len(e.Routes()), "quota routes must not register under kruize")
}

func TestQuotaPlugin_RegisterRoutes_RegistersWhenNativeEngine(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	syncPluginConfigFromEnv(t)

	p := &QuotaPlugin{}
	require.False(t, plugin.EnabledFor(plugin.KruizePluginName))

	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	before := len(e.Routes())
	p.RegisterRoutes(v1)
	assert.Greater(t, len(e.Routes()), before, "quota route should register under native engine")
}

func TestQuotaPlugin_HookAfterNamespaceOnly(t *testing.T) {
	t.Parallel()

	p := &QuotaPlugin{}
	types := p.HookAfterCSVTypes()
	require.Len(t, types, 1)
	assert.Equal(t, "namespace", types[0])
	assert.NotContains(t, types, "container")
}

func TestQuotaPlugin_AfterIngest_NoErrorOnEmptyCluster(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	syncPluginConfigFromEnv(t)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	p := &QuotaPlugin{}

	err := p.AfterIngest(ctx, pool, nil, "org-quota-hook", "550e8400-e29b-41d4-a716-446655440099")
	require.NoError(t, err)
}

func quotaIngestHookNames(t *testing.T) []string {
	t.Helper()
	hooks := plugin.ByTrait[plugin.IngestHook]()
	out := make([]string, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, h.Name())
	}
	return out
}

func TestQuotaPlugin_DisabledExcludedFromIngestHooks(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "quota")
	syncPluginConfigFromEnv(t)

	assert.NotContains(t, quotaIngestHookNames(t), "quota")
}

func TestQuotaPlugin_EnabledIncludedInIngestHooks(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	syncPluginConfigFromEnv(t)

	assert.Contains(t, quotaIngestHookNames(t), "quota")
}
