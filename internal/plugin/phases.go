package plugin

import (
	"context"
	"sort"
)

const (
	// PhaseProduce generates recommendations from raw metrics and digests.
	PhaseProduce = 1
	// PhaseEnrich annotates, classifies, or enhances Phase 1 outputs.
	PhaseEnrich = 2
	// PhaseOptimize performs cross-entity aggregation requiring a global view.
	PhaseOptimize = 3
)

// BasePlugin provides default Phase 1 for plugins that do not override Phase().
type BasePlugin struct{}

func (BasePlugin) Phase() int { return PhaseProduce }

// sortPluginsByPhase returns plugins ordered by Phase ascending, then Name ascending
// within the same phase for deterministic execution.
func sortPluginsByPhase(plugins []Plugin) []Plugin {
	if len(plugins) == 0 {
		return nil
	}
	sorted := make([]Plugin, len(plugins))
	copy(sorted, plugins)
	sort.Slice(sorted, func(i, j int) bool {
		pi, pj := normalizePhase(sorted[i].Phase()), normalizePhase(sorted[j].Phase())
		if pi != pj {
			return pi < pj
		}
		return sorted[i].Name() < sorted[j].Name()
	})
	return sorted
}

func normalizePhase(phase int) int {
	if phase < PhaseProduce || phase > PhaseOptimize {
		return PhaseProduce
	}
	return phase
}

// groupEnabledByPhase returns enabled plugins grouped by phase. Each phase slice
// is sorted by plugin name. Phases without enabled plugins are omitted.
func groupEnabledByPhase() map[int][]Plugin {
	enabled := Enabled()
	groups := make(map[int][]Plugin)
	for _, p := range enabled {
		ph := normalizePhase(p.Phase())
		groups[ph] = append(groups[ph], p)
	}
	return groups
}

// ExecuteInPhases runs fn for each enabled plugin in phase order (1, 2, 3).
// All plugins in a phase complete before the next phase begins. Within a phase,
// plugins run in name-sorted order.
func ExecuteInPhases(ctx context.Context, fn func(context.Context, Plugin) error) error {
	byPhase := groupEnabledByPhase()
	for phase := PhaseProduce; phase <= PhaseOptimize; phase++ {
		for _, p := range byPhase[phase] {
			if err := fn(ctx, p); err != nil {
				return err
			}
		}
	}
	return nil
}
