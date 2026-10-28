package pvc

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// DefaultTerms returns the documented PVC term windows. This is the single
// source of the PVC term contract (ADR-0346): the server's PVC plugin
// DefaultTerms() delegates here, and the robne CLI resolves its effective
// PVC terms from the same table. Values: short 7d (min 3), medium 30d
// (min 14), long 90d (min 30); no decay — the WLS engine applies its own
// internal weighting during slope computation.
func DefaultTerms() []types.TermConfig {
	return []types.TermConfig{
		{Name: "short", WindowDays: 7, MinDataDays: 3},
		{Name: "medium", WindowDays: 30, MinDataDays: 14},
		{Name: "long", WindowDays: 90, MinDataDays: 30},
	}
}
