package kruize

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

// KruizePlugin is the legacy Kruize recommendation engine.
// When enabled, the registry disables all native plugins (mutual exclusivity).
// The actual Kruize processing logic remains in internal/services/ and internal/utils/kruize/
// and is controlled by cfg.UseNativeEngine. This plugin's registration triggers
// the registry's exclusivity enforcement.
type KruizePlugin struct{}

func init() {
	plugin.Register(&KruizePlugin{})
}

func (p *KruizePlugin) Name() string { return "kruize" }

func (p *KruizePlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }
