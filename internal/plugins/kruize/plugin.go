// Package kruize implements the legacy Kruize recommendation engine plugin.
//
// Kruize is an external Java-based optimization engine (Apache-licensed, from
// the Autotune project) that was the original recommendation backend for ROS-OCP.
// It has been superseded by the native Go engine for most recommendation domains
// but remains available for backward compatibility.
//
// # Mutual Exclusivity
//
// When Kruize is enabled (ROS_ENABLED_PLUGINS=kruize or the deprecated
// ROS_USE_NATIVE_ENGINE=false), the registry disables all native plugins.
// This is because Kruize and the native engine produce recommendations in
// different formats and store them in different tables. Running both
// simultaneously would produce conflicting results.
//
// # Architecture
//
// Unlike native plugins, Kruize processing logic lives outside this package:
//   - internal/services/ — Kafka consumer routing to Kruize
//   - internal/utils/kruize/ — HTTP client for the Kruize API
//   - Kruize itself runs as a separate container (Java/Quarkus)
//
// This plugin exists solely to participate in the registry for enable/disable
// semantics and mutual exclusivity enforcement.
//
// # Traits Implemented
//
//   - [plugin.Plugin] — base only (no CSVIngestor, no APIProvider, no TermProvider)
//
// Note: This plugin does NOT implement [plugin.TermProvider] because Kruize
// manages its own internal time windowing independently of ros-ocp-backend.
package kruize

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

// KruizePlugin represents the legacy Kruize recommendation engine.
// It is mutually exclusive with all native plugins.
type KruizePlugin struct{}

func init() {
	plugin.Register(&KruizePlugin{})
}

func (p *KruizePlugin) Name() string { return plugin.KruizePluginName }

func (p *KruizePlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }
