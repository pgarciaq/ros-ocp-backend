package plugin

import (
	"os"
	"strings"
	"sync"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

const (
	// KruizePluginName is the registry name for the legacy Kruize engine plugin.
	KruizePluginName = "kruize"

	envEnabledPlugins  = "ROS_ENABLED_PLUGINS"
	envDisabledPlugins = "ROS_DISABLED_PLUGINS"
)

var (
	regMu      sync.RWMutex
	registry   []Plugin
	bootOnce   sync.Once
	kruizeOnce sync.Once
)

// Register appends a plugin to the process-wide registry. Call from plugin init().
//
// Convention: always register pointer receivers so traits are detected correctly,
// e.g. plugin.Register(&MyPlugin{}).
//
// Register panics if p is nil.
func Register(p Plugin) {
	if p == nil {
		panic("plugin.Register: cannot register nil plugin")
	}
	regMu.Lock()
	defer regMu.Unlock()
	registry = append(registry, p)
}

// All returns every registered plugin (enabled or not).
func All() []Plugin {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Plugin, len(registry))
	copy(out, registry)
	return out
}

// Enabled returns plugins that are active after env filtering and kruize exclusivity.
func Enabled() []Plugin {
	regMu.RLock()
	regCopy := make([]Plugin, len(registry))
	copy(regCopy, registry)
	regMu.RUnlock()

	var candidates []Plugin
	for _, p := range regCopy {
		if p.Enabled() {
			candidates = append(candidates, p)
		}
	}

	var kruizePlugins []Plugin
	var others []Plugin
	for _, p := range candidates {
		if p.Name() == KruizePluginName {
			kruizePlugins = append(kruizePlugins, p)
		} else {
			others = append(others, p)
		}
	}

	if len(kruizePlugins) > 0 {
		if len(others) > 0 {
			kruizeOnce.Do(func() {
				logging.GetLogger().Warn(
					"plugin registry: kruize is enabled; skipping other plugins to avoid duplicate or conflicting recommendations",
				)
			})
		}
		return kruizePlugins
	}

	return candidates
}

// ByTrait returns enabled plugins that implement trait T (an interface type).
func ByTrait[T any]() []T {
	var out []T
	for _, p := range Enabled() {
		if t, ok := any(p).(T); ok {
			out = append(out, t)
		}
	}
	return out
}

// APIProviders returns plugins implementing APIProvider from the full registry where each plugin's
// Enabled() is true, without applying Kruize mutual exclusivity. Ingestion traits still use [Enabled]
// (exclusive), but HTTP routes such as namespace listings must remain available when Kruize owns
// container processing.
func APIProviders() []APIProvider {
	regMu.RLock()
	regCopy := make([]Plugin, len(registry))
	copy(regCopy, registry)
	regMu.RUnlock()

	var out []APIProvider
	for _, p := range regCopy {
		if !p.Enabled() {
			continue
		}
		if ap, ok := p.(APIProvider); ok {
			out = append(out, ap)
		}
	}
	return out
}

// EnabledFor reports whether a plugin name is enabled from ROS_ENABLED_PLUGINS /
// ROS_DISABLED_PLUGINS before kruize exclusivity is applied.
//
// Semantics:
//   - If ROS_ENABLED_PLUGINS is non-empty: only listed names are enabled (allowlist).
//   - Otherwise: all plugins are enabled by default except "kruize", then
//     ROS_DISABLED_PLUGINS is applied as a blocklist.
func EnabledFor(name string) bool {
	allow := parsePluginSet(os.Getenv(envEnabledPlugins))
	deny := parsePluginSet(os.Getenv(envDisabledPlugins))

	if len(allow) > 0 {
		return allow[name]
	}
	if deny[name] {
		return false
	}
	if name == KruizePluginName {
		return false
	}
	return true
}

// ApplyLegacyUseNativeEngineEnv maps ROS_USE_NATIVE_ENGINE=false to ROS_ENABLED_PLUGINS=kruize when
// the allowlist is unset so legacy deployments keep working. Prefer setting ROS_ENABLED_PLUGINS=kruize
// explicitly. Call once from main before subsystem code reads EnabledFor / Enabled.
func ApplyLegacyUseNativeEngineEnv(useNativeEngine bool) {
	if useNativeEngine {
		return
	}
	if strings.TrimSpace(os.Getenv(envEnabledPlugins)) != "" {
		return
	}
	if err := os.Setenv(envEnabledPlugins, KruizePluginName); err != nil {
		logging.GetLogger().Warnf("plugin registry: could not set %s: %v", envEnabledPlugins, err)
		return
	}
	logging.GetLogger().Warn(
		"ROS_USE_NATIVE_ENGINE=false is deprecated; use ROS_ENABLED_PLUGINS=kruize instead",
	)
}

func parsePluginSet(raw string) map[string]bool {
	out := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

// Boot runs one-time plugin registry startup logic. Safe to call multiple times.
func Boot() {
	bootOnce.Do(func() {
		logging.GetLogger().WithField("registered_plugin_count", len(All())).Info("plugin registry bootstrapped")
	})
}

// Init is an alias for [Boot].
func Init() {
	Boot()
}
