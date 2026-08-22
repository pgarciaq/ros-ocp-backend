package vm

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func TestVMPlugin_Implements_CSVIngestor(t *testing.T) {
	t.Parallel()

	var _ plugin.CSVIngestor = (*VMPlugin)(nil)
}

func TestVMPlugin_Implements_RetentionProvider(t *testing.T) {
	t.Parallel()

	var _ plugin.RetentionProvider = (*VMPlugin)(nil)
}

func TestVMPlugin_Priority_Is40(t *testing.T) {
	t.Parallel()

	p := &VMPlugin{}
	assert.Equal(t, 40, p.Priority())
}

func TestVMPlugin_Name(t *testing.T) {
	t.Parallel()

	p := &VMPlugin{}
	assert.Equal(t, "vm", p.Name())
}

func TestVMPlugin_RetentionTables_IncludesAllTables(t *testing.T) {
	t.Parallel()

	p := &VMPlugin{}
	assert.Equal(t,
		[]string{"daily_vm_digests", "vm_recommendations", "vm_recommendation_history", "hourly_vm_digests"},
		p.RetentionTables(),
	)
}

func TestVMPlugin_SupportedCSVTypes(t *testing.T) {
	t.Parallel()

	p := &VMPlugin{}
	assert.Equal(t,
		[]string{string(types.PayloadTypeVM), string(types.PayloadTypeVMGPU), string(types.PayloadTypeVMPVC)},
		p.SupportedCSVTypes(),
	)
}

func TestVMPlugin_RegisterRoutes_WhenDisabled_NoRoutes(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "vm")
	config.ResetForTest()
	_ = config.GetConfig()

	p := &VMPlugin{}
	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	before := len(e.Routes())
	p.RegisterRoutes(v1)
	assert.Equal(t, before, len(e.Routes()), "VM routes must not register when vm plugin is disabled")
}

func TestVMPlugin_RegisterRoutes_WhenKruizeEnabled_NoRoutes(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "kruize")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()
	_ = config.GetConfig()

	p := &VMPlugin{}
	require.True(t, plugin.EnabledFor(plugin.KruizePluginName))

	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	before := len(e.Routes())
	p.RegisterRoutes(v1)
	assert.Equal(t, before, len(e.Routes()), "VM routes must not register when kruize is the active engine")
}

func TestVMPlugin_V1_NoAPIEnricher_DualWriteOnIngest(t *testing.T) {
	t.Parallel()

	p := &VMPlugin{}
	_, isEnricher := interface{}(p).(plugin.APIEnricher)
	assert.False(t, isEnricher, "VM plugin must not implement APIEnricher")

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	pluginGo, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "plugin.go"))
	require.NoError(t, err)
	body := string(pluginGo)
	assert.Contains(t, body, "ProducesBusinessHoursDigests")
	assert.Contains(t, body, "ScheduleTypeBusinessHours")
	assert.Contains(t, body, "BuildDailyVMDigestsIfWeight")
	assert.NotContains(t, body, "APIEnricher")
}
