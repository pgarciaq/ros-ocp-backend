package main

import (
	"fmt"

	libengine "github.com/redhatinsights/ros-ocp-backend/librobne/engine"
	libpvc "github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	libvm "github.com/redhatinsights/ros-ocp-backend/librobne/vm"
)

// termBearingPlugins lists the plugins whose engines consume term windows.
// quota, cluster-quota, snapshot, and the HCP capacity advisory have no
// terms by design (ADR-0346); plugin_terms entries for them are errors
// (validated in the config layer).
var termBearingPlugins = []string{"container", "namespace", "node", "gpu", "vm", "pvc"}

// effectiveEngineTerms resolves the term table for one plugin (#705,
// ADR-0346): plugin_terms[plugin] when configured, else the plugin's
// documented table — the same resolution the server runs (tenant override
// ‖ plugin default) minus the tiers robne does not have. Nothing falls
// through between plugins: the historical fall-through of container terms
// into VM/PVC was the #705 root cause.
//
// The top-level terms: list stays container-family-scoped (container,
// namespace, node, gpu) for back-compat; VM and PVC resolve only through
// plugin_terms or their documented tables.
func effectiveEngineTerms(plugin string, cfg fileConfig) []types.TermConfig {
	if ts, ok := cfg.PluginTerms[plugin]; ok && len(ts) > 0 {
		return termsToEngine(ts)
	}
	switch plugin {
	case "vm":
		return vmDefaultTermsEngine()
	case "pvc":
		return libpvc.DefaultTerms()
	default:
		if len(cfg.Terms) > 0 {
			return termsToEngine(cfg.Terms)
		}
		return libengine.DefaultTerms()
	}
}

// vmDefaultTermsEngine converts the librobne VM table (the single source,
// shared with the server's plugin) to the engine term shape. No decay: the
// VM engine does not decay-weight.
func vmDefaultTermsEngine() []types.TermConfig {
	ws := libvm.DefaultVMTermWindows()
	out := make([]types.TermConfig, len(ws))
	for i, w := range ws {
		out[i] = types.TermConfig{
			Name:        w.Name,
			WindowDays:  w.LookbackDays,
			MinDataDays: w.MinDataDays,
		}
	}
	return out
}

// maxEffectiveLookbackDays returns the largest window across all six
// plugins' effective tables (configured or documented). robne's Path-A
// digest SELECT window is sized from this, not the container table's
// maximum — the container maximum (15d) would truncate VM (30d) and PVC
// (90d) long-term data.
func maxEffectiveLookbackDays(cfg fileConfig) int {
	max := 0
	for _, p := range termBearingPlugins {
		for _, t := range effectiveEngineTerms(p, cfg) {
			if t.WindowDays > max {
				max = t.WindowDays
			}
		}
	}
	return max
}

// pluginMaxWindowDays mirrors the server plugins' MaxWindowDays() trait
// values — the Settings-API validation bound robne matches (ADR-0346).
// If a plugin's bound changes server-side, this map follows in the same
// change.
var pluginMaxWindowDays = map[string]int{
	"container": 90, "namespace": 90, "node": 90, "gpu": 90,
	"vm": 90, "pvc": 365,
}

// validatePluginTerms enforces the plugin_terms contract (spec §2):
// only the six term-bearing plugins are valid keys (quota, cluster-quota,
// snapshot, and hcp have no term windows by design); terms: plus
// plugin_terms.container is ambiguous; entries carry explicit windows
// within the server's bounds. Whole-key replacement per plugin entry —
// a present plugin key must carry its complete term list.
func validatePluginTerms(cfg fileConfig) error {
	if len(cfg.PluginTerms) == 0 {
		return nil
	}
	if len(cfg.Terms) > 0 {
		if _, ok := cfg.PluginTerms["container"]; ok {
			return fmt.Errorf("terms: and plugin_terms.container are both set — configure one (terms: is container-family-scoped)")
		}
	}
	for name, ts := range cfg.PluginTerms {
		valid := false
		for _, p := range termBearingPlugins {
			if p == name {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("plugin_terms: unknown plugin %q (valid: container, namespace, node, gpu, vm, pvc — quota, cluster-quota, snapshot, and hcp have no term windows)", name)
		}
		if len(ts) == 0 {
			return fmt.Errorf("plugin_terms.%s: empty term list — omit the plugin to use its documented table", name)
		}
		maxWin := pluginMaxWindowDays[name]
		for _, t := range ts {
			if t.WindowDays < 1 || t.WindowDays > maxWin {
				return fmt.Errorf("plugin_terms.%s: window_days must be in 1..%d, got %d", name, maxWin, t.WindowDays)
			}
			if t.MinDataDays < 1 || t.MinDataDays > t.WindowDays {
				return fmt.Errorf("plugin_terms.%s: min_data_days must be in 1..window_days (%d) — the entry replaces the plugin's table, specify it fully", name, t.WindowDays)
			}
		}
	}
	return nil
}
