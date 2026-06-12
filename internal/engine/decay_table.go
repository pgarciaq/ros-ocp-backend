package engine

import (
	"math"
	"sync"
)

var decayTables sync.Map // map[int][]float64, keyed by halfLifeHours (integer)

// DeriveDecayHalfLifeHours returns the default decay half-life for a term window.
// Formula: window_days × 0.5 × 24h = window_days × 12.
func DeriveDecayHalfLifeHours(windowDays int) float64 {
	return float64(windowDays * 12)
}

// decayTableLookup returns the precomputed weight for the given age and half-life.
// halfLifeHours must be a positive integer (as produced by window_days * 12).
// Returns the precomputed value if available, falls back to math.Exp for
// non-integer or out-of-range half-lives.
func decayTableLookup(ageHours int, halfLifeHours int) float64 {
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
