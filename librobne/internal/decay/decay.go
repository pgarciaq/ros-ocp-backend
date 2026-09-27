// Package decay holds decay-table machinery shared by librobne's types, node,
// and PVC packages. Its internal import path keeps the evaluator out of the
// library's public API.
package decay

import (
	"math"
	"sync"
)

const maxTableEntries = 100_000

// tables contains complete immutable slices keyed by integer half-life hours.
var tables sync.Map

// Evaluator binds one half-life to its immutable table so a row loop resolves
// the cached table once before iteration instead of once per row.
// Its zero value applies no decay, like a zero half-life.
type Evaluator struct {
	halfLifeHours   float64
	halfLifeInt     int
	maxAge          int
	integerHalfLife bool
	table           []float64
}

// NewEvaluator prepares decay state for reuse across rows with one half-life.
func NewEvaluator(halfLifeHours float64) Evaluator {
	evaluator := Evaluator{halfLifeHours: halfLifeHours}
	if halfLifeHours <= 0 {
		return evaluator
	}

	evaluator.halfLifeInt = int(math.Round(halfLifeHours))
	if evaluator.halfLifeInt <= 0 || float64(evaluator.halfLifeInt) != halfLifeHours {
		return evaluator
	}

	evaluator.integerHalfLife = true
	evaluator.maxAge = evaluator.halfLifeInt * 2
	if evaluator.maxAge <= maxTableEntries {
		evaluator.table = tableForHalfLife(evaluator.halfLifeInt)
	}
	return evaluator
}

// Weight returns the decay weight for one age using the evaluator's fixed
// half-life, preserving the single-value Weight function's numeric behavior.
func (evaluator Evaluator) Weight(ageHours float64) float64 {
	if evaluator.halfLifeHours <= 0 || evaluator.halfLifeInt <= 0 {
		return 1.0
	}

	if !evaluator.integerHalfLife {
		return math.Exp(-ageHours * math.Ln2 / evaluator.halfLifeHours)
	}

	ageInt := int(math.Round(ageHours))
	if ageInt < 0 {
		ageInt = 0
	}
	if evaluator.maxAge > maxTableEntries {
		if ageInt > evaluator.maxAge {
			return 0
		}
		return math.Exp(-math.Ln2 * float64(ageInt) / float64(evaluator.halfLifeInt))
	}
	if ageInt >= len(evaluator.table) {
		return 0
	}
	return evaluator.table[ageInt]
}

// Weight preserves the single-value behavior for callers that do not need to
// reuse an evaluator across a row loop, including its arithmetic order.
func Weight(ageHours, halfLifeHours float64) float64 {
	if halfLifeHours <= 0 {
		return 1.0
	}
	ageInt := int(math.Round(ageHours))
	halfLifeInt := int(math.Round(halfLifeHours))
	if halfLifeInt <= 0 {
		return 1.0
	}
	if float64(halfLifeInt) == halfLifeHours {
		return TableLookup(ageInt, halfLifeInt)
	}
	return math.Exp(-ageHours * math.Ln2 / halfLifeHours)
}

// TableLookup returns exp(-ln2 × age / halfLife) from the precomputed table.
// Ages beyond halfLife×2 return zero; large tables use the equivalent direct
// exponential fallback without retaining an oversized slice.
func TableLookup(ageHours, halfLifeHours int) float64 {
	if halfLifeHours <= 0 {
		return 1.0
	}
	if ageHours < 0 {
		ageHours = 0
	}

	maxAge := halfLifeHours * 2
	if maxAge > maxTableEntries {
		if ageHours > maxAge {
			return 0
		}
		return math.Exp(-math.Ln2 * float64(ageHours) / float64(halfLifeHours))
	}

	table := tableForHalfLife(halfLifeHours)
	if ageHours >= len(table) {
		return 0
	}
	return table[ageHours]
}

func tableForHalfLife(halfLifeHours int) []float64 {
	v, ok := tables.Load(halfLifeHours)
	if !ok {
		maxAge := halfLifeHours * 2
		table := make([]float64, maxAge+1)
		k := -math.Ln2 / float64(halfLifeHours)
		for h := 0; h <= maxAge; h++ {
			table[h] = math.Exp(k * float64(h))
		}
		actual, _ := tables.LoadOrStore(halfLifeHours, table)
		v = actual
	}
	return v.([]float64)
}
