// Decay weight lookup tables (ADR-0288).
//
// This file implements precomputed exponential decay weight lookup tables
// keyed by integer half-life hours. DecayWeight() quantizes age and half-life to
// whole hours and calls DecayTableLookup instead of math.Exp on every digest row.
//
// Tables are built lazily via sync.Map on first use per distinct half-life (typically
// 2–3 per Kafka batch). ros-ocp-backend runs per-batch, not as a long-lived daemon,
// so compile-time go:generate embedding was rejected — microseconds of lazy build cost
// is acceptable. A single normalized table was also rejected to preserve the
// per-term decay_halflife_hours tuning knob.
//
// Integer-hour quantization introduces at most ~0.2% weight error vs continuous hours;
// negligible for recommendation quality (see decay_test.go).
package core

import (
	"math"
	"sync"
)

// decayTables caches []float64 slices keyed by integer halfLifeHours.
// Built lazily; persists for process lifetime (one batch invocation).
var decayTables sync.Map

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
	if halfLifeHours <= 0 {
		return 1.0
	}
	if ageHours < 0 {
		ageHours = 0
	}

	v, ok := decayTables.Load(halfLifeHours)
	if !ok {
		// window = halfLife/12 days, maxAge = window*24 = halfLife*2
		maxAge := halfLifeHours * 2
		table := make([]float64, maxAge+1)
		k := -math.Ln2 / float64(halfLifeHours)
		for h := 0; h <= maxAge; h++ {
			table[h] = math.Exp(k * float64(h))
		}
		actual, _ := decayTables.LoadOrStore(halfLifeHours, table)
		v = actual
	}

	table := v.([]float64)
	if ageHours >= len(table) {
		return 0 // beyond window, negligible weight
	}
	return table[ageHours]
}
