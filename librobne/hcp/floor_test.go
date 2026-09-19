// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package hcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Locked values (#584 floors decision): relative 70% of current request,
// absolute 100m CPU / 128MiB memory. Distributions from the live lab
// package 20260914T153624 (idle; requests are install defaults, usage
// bursts dwarf them) support the relative-first design: on lab-shaped
// data the absolute floor governs, on production-shaped data (real
// requests) the relative floor governs. Both are asserted below.
func TestControlPlaneFloorConstants(t *testing.T) {
	assert.Equal(t, int64(100), ControlPlaneCPUFloorMC)
	assert.Equal(t, int64(131072), ControlPlaneMemFloorKiB)
	assert.Equal(t, 70, ControlPlaneFloorPct)
}

func TestEffectiveFloor(t *testing.T) {
	cases := []struct {
		name    string
		current int64
		abs     int64
		pct     int
		want    int64
		why     string
	}{
		// Production-shaped: real 1000m request → relative 700m governs.
		{"relative governs real requests", 1000, 100, 70, 700, "70% of current request must win over the absolute floor"},
		// Lab-shaped: ceremonial 10m request → relative 7m loses to absolute 100m.
		{"absolute governs ceremonial requests", 10, 100, 70, 100, "absolute floor must catch near-zero current requests"},
		// No current request (blank join gap) → absolute floor, never zero.
		{"zero current request", 0, 100, 70, 100, "missing current request must not zero the floor"},
		// Integer truncation biases the relative leg down; the absolute leg
		// dominates small currents by design, so truncation never escapes.
		{"truncation stays below absolute", 101, 100, 70, 100, "101*70/100 truncates to 70, absolute 100 must still win"},
		// Exact tie is stable regardless of branch order.
		{"tie is stable", 100, 70, 70, 70, "70% of 100 ties absolute 70"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, EffectiveFloor(tc.current, tc.abs, tc.pct), "%s", tc.why)
	}
}
