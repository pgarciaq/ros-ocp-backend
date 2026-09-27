// Decay weight lookup tables (ADR-0288).
//
// The table and evaluator implementation lives in the librobne/internal/decay
// package so the container, node, and PVC loops can share it without expanding
// the exported types API. This file retains the compatibility lookup entrypoint.
//
// Tables are built lazily via sync.Map on first use per distinct half-life (typically
// 2–3 per Kafka batch). ros-ocp-backend runs per-batch, not as a long-lived daemon,
// so compile-time go:generate embedding was rejected — microseconds of lazy build cost
// is acceptable. A single normalized table was also rejected to preserve the
// per-term decay_halflife_hours tuning knob.
//
// Integer-hour quantization introduces at most ~0.2% weight error vs continuous hours;
// negligible for recommendation quality (see decay_test.go).
package types

import (
	"github.com/redhatinsights/ros-ocp-backend/librobne/internal/decay"
)

// DeriveDecayHalfLifeHours returns the default decay half-life for a term window
// when decay_halflife_hours is NULL in org_recommendation_terms.
// Formula: window_days × 0.5 × 24h = window_days × 12.
func DeriveDecayHalfLifeHours(windowDays int) float64 {
	return float64(windowDays * 12)
}

// DecayTableLookup returns exp(-ln2 × age / halfLife) from a precomputed table.
// halfLifeHours must be a positive integer (plugin defaults and window_days×12
// auto-derive produce whole-hour values). Ages beyond halfLife×2 return 0.
func DecayTableLookup(ageHours int, halfLifeHours int) float64 {
	return decay.TableLookup(ageHours, halfLifeHours)
}
