package plugin

import (
	"strings"
	"sync"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
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
// Registration order across packages is non-deterministic (Go spec does not
// guarantee init() ordering). This is intentional: plugins are independent,
// route matching is path-based, and CSV claim sets don't overlap between plugins.
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
	cfg := config.GetConfig()
	allow := parsePluginSet(cfg.EnabledPlugins)
	deny := parsePluginSet(cfg.DisabledPlugins)

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

// ApplyLegacyUseNativeEngineEnv reconciles legacy ROS_USE_NATIVE_ENGINE with ROS_ENABLED_PLUGINS.
// Call once from main before subsystem code reads EnabledFor / Enabled.
//
// When useNativeEngine is false: forces ROS_ENABLED_PLUGINS=kruize (overwrites any user value) and logs deprecation.
// When useNativeEngine is true: strips "kruize" from a non-empty ROS_ENABLED_PLUGINS allowlist if present
// (kruize cannot run alongside native plugins); logs a warning when stripping.
func ApplyLegacyUseNativeEngineEnv(useNativeEngine bool) {
	log := logging.GetLogger()
	cfg := config.GetConfig()
	if !useNativeEngine {
		cfg.EnabledPlugins = KruizePluginName
		log.Warn("ROS_USE_NATIVE_ENGINE=false is deprecated; forcing ROS_ENABLED_PLUGINS=kruize")
		return
	}

	raw := strings.TrimSpace(cfg.EnabledPlugins)
	if raw == "" {
		return
	}
	newVal, removed := stripPluginNameFromAllowlist(raw, KruizePluginName)
	if !removed {
		return
	}
	log.Warn("ROS_ENABLED_PLUGINS listed kruize while the native engine is enabled; removing kruize from the allowlist (mutually exclusive)")
	cfg.EnabledPlugins = newVal
}

func stripPluginNameFromAllowlist(raw, name string) (newList string, removed bool) {
	var kept []string
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if p == name {
			removed = true
			continue
		}
		kept = append(kept, p)
	}
	return strings.Join(kept, ","), removed
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
// It validates that no two enabled CSVIngestors claim the same CSV type and
// warns if the Kruize plugin is enabled without native plugins.
func Boot() {
	bootOnce.Do(func() {
		logging.GetLogger().WithField("registered_plugin_count", len(All())).Info("plugin registry bootstrapped")

		validateCSVTypeClaims()
		warnKruizeEnabled()
	})
}

// Init is an alias for [Boot].
func Init() {
	Boot()
}

// validateCSVTypeClaims fatals if two enabled CSVIngestors claim the same type.
func validateCSVTypeClaims() {
	log := logging.GetLogger()
	claims := make(map[string]string) // csvType → plugin name
	for _, ing := range ByTrait[CSVIngestor]() {
		for _, ct := range ing.SupportedCSVTypes() {
			if prev, exists := claims[ct]; exists {
				log.Fatalf(
					"plugin registry collision: CSV type %q claimed by both %q and %q",
					ct, prev, ing.Name(),
				)
			}
			claims[ct] = ing.Name()
		}
	}
}

// warnKruizeEnabled emits a startup warning when the Kruize plugin is enabled,
// since it requires an external Kruize service to be reachable.
func warnKruizeEnabled() {
	if !EnabledFor(KruizePluginName) {
		return
	}
	logging.GetLogger().Warn(
		"plugin registry: Kruize plugin is enabled. " +
			"The external Kruize/Autotune service must be reachable at KRUIZE_URL " +
			"for recommendation processing to succeed. " +
			"Native plugins are disabled (mutual exclusivity).",
	)
}
