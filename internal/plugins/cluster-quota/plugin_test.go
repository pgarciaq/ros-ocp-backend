package clusterquota

import (
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func syncPluginConfigFromEnv(t *testing.T) {
	t.Helper()
	config.ResetForTest()
	_ = config.GetConfig()
}

func TestClusterQuotaPlugin_Metadata(t *testing.T) {
	t.Parallel()
	p := &ClusterQuotaPlugin{}
	assert.Equal(t, "cluster-quota", p.Name())
	assert.Equal(t, plugin.PhaseProduce, p.Phase())
	assert.Equal(t, 36, p.Priority())
	assert.Equal(t, []string{string(types.PayloadTypeClusterQuota)}, p.SupportedCSVTypes())
	assert.Equal(t, []string{
		"cluster_quota_recommendation_sets",
		"cluster_quota_recommendation_history",
		"daily_cluster_quota_digests",
	}, p.RetentionTables())
}

func TestClusterQuotaPlugin_RegisterRoutes_NativeEngine(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	syncPluginConfigFromEnv(t)

	p := &ClusterQuotaPlugin{}
	require.False(t, plugin.EnabledFor(plugin.KruizePluginName))

	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	before := len(e.Routes())
	p.RegisterRoutes(v1)
	assert.Greater(t, len(e.Routes()), before)
}
