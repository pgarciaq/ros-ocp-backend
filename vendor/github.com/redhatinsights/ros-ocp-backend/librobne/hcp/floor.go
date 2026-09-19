// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package hcp

// Locked guardrail floor values (#584 floors decision, derived from the
// live-lab distributions in floor_test.go): relative 70% of current
// request governs production-shaped data, absolute 100m CPU / 128MiB
// memory governs ceremonial/blank requests. Uniform across the strict
// set — no per-component tiers without evidence.
const (
	// ControlPlaneCPUFloorMC is the absolute CPU backstop in millicores.
	ControlPlaneCPUFloorMC = int64(100)
	// ControlPlaneMemFloorKiB is the absolute memory backstop in KiB (128MiB).
	ControlPlaneMemFloorKiB = int64(131072)
	// ControlPlaneFloorPct is the relative floor: percent of current
	// request below which a recommendation never falls.
	ControlPlaneFloorPct = 70
)

// EffectiveFloor returns max(absolute, pct% of current), truncating the
// relative leg. Truncation biases down; the absolute leg dominates small
// currents by design, so truncation never escapes below it.
func EffectiveFloor(current, absolute int64, pct int) int64 {
	rel := current * int64(pct) / 100
	if rel > absolute {
		return rel
	}
	return absolute
}
