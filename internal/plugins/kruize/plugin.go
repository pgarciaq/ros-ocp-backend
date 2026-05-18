package kruize

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

// KruizePlugin is the legacy Kruize recommendation engine.
// When enabled (ROS_ENABLED_PLUGINS=kruize, or deprecated ROS_USE_NATIVE_ENGINE=false), the registry
// disables all native plugins (mutual exclusivity). Processing logic remains in internal/services/
// and internal/utils/kruize/.
type KruizePlugin struct{}

func init() {
	plugin.Register(&KruizePlugin{})
}

func (p *KruizePlugin) Name() string { return plugin.KruizePluginName }

func (p *KruizePlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }
