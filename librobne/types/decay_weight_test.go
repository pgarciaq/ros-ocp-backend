package types

import (
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/librobne/internal/decay"
)

func TestDecayWeightEvaluator_MatchesLegacyBehavior(t *testing.T) {
	tests := []struct {
		name     string
		ageHours float64
		halfLife float64
	}{
		{name: "medium half-life", ageHours: 168, halfLife: 336},
		{name: "short half-life", ageHours: 168, halfLife: 84},
		{name: "half-life boundary", ageHours: 168, halfLife: 168},
		{name: "zero half-life", ageHours: 168, halfLife: 0},
		{name: "negative half-life", ageHours: 168, halfLife: -24},
		{name: "sub-hour rounds to zero", ageHours: 12, halfLife: 0.49},
		{name: "non-integer half-life", ageHours: 48.5, halfLife: 167.3},
		{name: "negative integer-path age clamps", ageHours: -23.6, halfLife: 168},
		{name: "negative non-integer-path age remains exponential", ageHours: -23.6, halfLife: 167.3},
		{name: "fractional age rounds down", ageHours: 335.49, halfLife: 168},
		{name: "fractional age rounds to cutoff", ageHours: 335.5, halfLife: 168},
		{name: "at table cutoff", ageHours: 336, halfLife: 168},
		{name: "fraction rounds above table cutoff", ageHours: 336.5, halfLife: 168},
		{name: "oversized table edge", ageHours: 100002, halfLife: 50001},
		{name: "oversized table cutoff", ageHours: 100002.5, halfLife: 50001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := legacyDecayWeight(tt.ageHours, tt.halfLife)
			evaluator := decay.NewEvaluator(tt.halfLife)
			// Prepared evaluation must preserve pre-#618 output bits for this edge case.
			if got := evaluator.Weight(tt.ageHours); math.Float64bits(got) != math.Float64bits(want) {
				t.Errorf("prepared weight bits = %016x, want legacy %016x (got %.17g, want %.17g)",
					math.Float64bits(got), math.Float64bits(want), got, want)
			}
			// Direct callers must retain the pre-#618 branch and arithmetic order.
			if got := DecayWeight(tt.ageHours, tt.halfLife); math.Float64bits(got) != math.Float64bits(want) {
				t.Errorf("compatibility weight bits = %016x, want legacy %016x (got %.17g, want %.17g)",
					math.Float64bits(got), math.Float64bits(want), got, want)
			}

			ageInt := int(math.Round(tt.ageHours))
			halfLifeInt := int(math.Round(tt.halfLife))
			wantTable := legacyDecayTableLookup(ageInt, halfLifeInt)
			// The exported integer lookup must preserve its independent compatibility contract.
			if got := DecayTableLookup(ageInt, halfLifeInt); math.Float64bits(got) != math.Float64bits(wantTable) {
				t.Errorf("table lookup bits = %016x, want legacy %016x (got %.17g, want %.17g)",
					math.Float64bits(got), math.Float64bits(wantTable), got, wantTable)
			}
		})
	}
}

func TestDecayWeightEvaluator_AlternatingHalfLives(t *testing.T) {
	halfLives := []float64{84, 168, 336}
	evaluators := make([]decay.Evaluator, len(halfLives))
	wants := make([]uint64, len(halfLives))
	for i, halfLife := range halfLives {
		evaluators[i] = decay.NewEvaluator(halfLife)
		wants[i] = math.Float64bits(legacyDecayWeight(168, halfLife))
	}
	// Distinct expected values ensure a shared/wrong table cannot be masked by collisions.
	if wants[0] == wants[1] || wants[1] == wants[2] || wants[0] == wants[2] {
		t.Fatal("fixture half-lives must produce distinct weights at age 168h")
	}

	for round := 0; round < 4; round++ {
		for i, evaluator := range evaluators {
			// Alternating evaluator use must not make one half-life reuse another's table.
			if got := evaluator.Weight(168); math.Float64bits(got) != wants[i] {
				t.Errorf("round %d, half-life %.0fh: weight bits = %016x, want %016x",
					round, halfLives[i], math.Float64bits(got), wants[i])
			}
		}
	}
}

func TestDecayWeightEvaluator_ConcurrentFirstUse(t *testing.T) {
	halfLives := [...]float64{1229, 1231, 1237}
	const workers = 24
	const iterations = 40
	var wg sync.WaitGroup
	errs := make(chan string, workers)
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		halfLife := halfLives[worker%len(halfLives)]
		go func() {
			defer wg.Done()
			ages := [...]float64{0, halfLife / 2, halfLife*2 + 0.6, -3.5}
			for i := 0; i < iterations; i++ {
				age := ages[i%len(ages)]
				evaluator := decay.NewEvaluator(halfLife)
				got := evaluator.Weight(age)
				want := legacyDecayWeight(age, halfLife)
				// Concurrent first use must publish only complete tables for the requested half-life.
				if math.Float64bits(got) != math.Float64bits(want) {
					errs <- fmt.Sprintf("half-life %.0f age %.2f: weight bits %016x, want %016x",
						halfLife, age, math.Float64bits(got), math.Float64bits(want))
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// legacyDecayWeight is the pre-#618 DecayWeight/DecayTableLookup branch and
// arithmetic order, frozen here as an independent exact-output oracle.
func legacyDecayWeight(ageHours, halfLifeHours float64) float64 {
	if halfLifeHours <= 0 {
		return 1
	}
	ageInt := int(math.Round(ageHours))
	hlInt := int(math.Round(halfLifeHours))
	if hlInt <= 0 {
		return 1
	}
	if float64(hlInt) != halfLifeHours {
		return math.Exp(-ageHours * math.Ln2 / halfLifeHours)
	}
	if ageInt < 0 {
		ageInt = 0
	}
	maxAge := hlInt * 2
	if maxAge > 100_000 {
		if ageInt > maxAge {
			return 0
		}
		return math.Exp(-math.Ln2 * float64(ageInt) / float64(hlInt))
	}
	if ageInt > maxAge {
		return 0
	}
	k := -math.Ln2 / float64(hlInt)
	return math.Exp(k * float64(ageInt))
}

func legacyDecayTableLookup(ageHours, halfLifeHours int) float64 {
	if halfLifeHours <= 0 {
		return 1
	}
	if ageHours < 0 {
		ageHours = 0
	}
	maxAge := halfLifeHours * 2
	if maxAge > 100_000 {
		if ageHours > maxAge {
			return 0
		}
		return math.Exp(-math.Ln2 * float64(ageHours) / float64(halfLifeHours))
	}
	if ageHours > maxAge {
		return 0
	}
	k := -math.Ln2 / float64(halfLifeHours)
	return math.Exp(k * float64(ageHours))
}
