package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"

	// Side-effect: registers all built-in plugins including kruize (cannot live in kruize/plugin_test.go — import cycle).
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins"
)

func TestRegistry_KruizeMutualExclusivity_withContainerPlugin(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "kruize,container")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	syncPluginConfigFromEnv(t)

	enabled := plugin.Enabled()
	require.Len(t, enabled, 1)
	assert.Equal(t, "kruize", enabled[0].Name())
}

func TestAPIProviders_keepsNamespaceRoutesEligibleUnderKruizeAllowlist(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "kruize")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	syncPluginConfigFromEnv(t)

	var seenNamespace bool
	for _, ap := range plugin.APIProviders() {
		if ap.Name() == "namespace" {
			seenNamespace = true
			break
		}
	}
	assert.True(t, seenNamespace, "namespace APIProvider should register while Kruize is allowlisted")
}
