package quota

import (
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
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

func TestQuotaPlugin_NoIngestHook(t *testing.T) {
	t.Parallel()

	hooks := plugin.ByTrait[plugin.IngestHook]()
	for _, h := range hooks {
		assert.NotEqual(t, "quota", h.Name(), "quota runs after container recs in report_processor, not via AfterIngest")
	}
}
