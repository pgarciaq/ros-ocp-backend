package vm

import (
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
		[]string{"daily_vm_digests", "vm_recommendations", "vm_recommendation_history"},
		p.RetentionTables(),
	)
}

func TestVMPlugin_SupportedCSVTypes(t *testing.T) {
	t.Parallel()

	p := &VMPlugin{}
	assert.Equal(t,
		[]string{string(types.PayloadTypeVM), string(types.PayloadTypeVMGPU)},
		p.SupportedCSVTypes(),
	)
}

func TestVMPlugin_RegisterRoutes_WhenDisabled_NoRoutes(t *testing.T) {
	t.Setenv("ROS_ENABLE_VM_RECS", "false")
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()
	_ = config.GetConfig()

	p := &VMPlugin{}
	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	before := len(e.Routes())
	p.RegisterRoutes(v1)
	assert.Equal(t, before, len(e.Routes()), "VM routes must not register when ROS_ENABLE_VM_RECS is false")
}

func TestVMPlugin_RegisterRoutes_WhenKruizeEnabled_NoRoutes(t *testing.T) {
	t.Setenv("ROS_ENABLE_VM_RECS", "true")
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
