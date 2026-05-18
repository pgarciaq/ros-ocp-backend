package plugin

import (
	"os"
	"strings"
	"sync"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

const (
	envEnabledPlugins  = "ROS_ENABLED_PLUGINS"
	envDisabledPlugins = "ROS_DISABLED_PLUGINS"

	pluginNameKruize = "kruize"
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
		if p.Name() == pluginNameKruize {
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
	if name == pluginNameKruize {
		return false
	}
	return true
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
