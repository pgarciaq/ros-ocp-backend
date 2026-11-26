package costdata

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ConvertCents converts cents by a float rate (legacy path, round-half-up).
// Deprecated for the FX path: #462 replaced it with ConvertCentsPPB (integer
// parts-per-billion) — the masu rate arrives as a string to keep Decimal
// digits, and the float64 round-trip loses them. Retained only where a float
// rate is genuinely the input.
func ConvertCents(cents int64, rate float64) int64 {
	if rate <= 0 {
		return cents
	}
	return int64(math.Floor(float64(cents)*rate + 0.5))
}

// ParseRateStringToPPB parses masu's exchange-rate string ("0.860000000000000"
// — stringified Decimal) into integer parts-per-billion without a float64
// round-trip (#462). Digits beyond the 9th decimal truncate: a rate is a
// ratio, and 1e-9 relative precision is far beyond any FX source's meaning.
// Realistic FX rates (< 1e6) give at most 15 digits — comfortably int64.
func ParseRateStringToPPB(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty exchange-rate string")
	}
	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	if hasFrac {
		if len(fracPart) > 9 {
			fracPart = fracPart[:9]
		} else {
			fracPart += strings.Repeat("0", 9-len(fracPart))
		}
	} else {
		fracPart = "000000000"
	}
	combined := intPart + fracPart
	if !isAllDigits(combined) {
		return 0, fmt.Errorf("invalid exchange-rate string %q", s)
	}
	ppb, err := strconv.ParseInt(combined, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse exchange-rate string %q: %w", s, err)
	}
	return ppb, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// ConvertCentsPPB converts cents by an integer parts-per-billion rate with
// round-half-up, using a decomposition that cannot overflow int64 (#462).
//
// The naive (cents*ppb + 5e8)/1e9 overflows int64 above ~$92M per amount
// (9.2e9 hundredth-cents x 1e9 ppb ~ int64 max) — fleet savings totals reach
// that class. The safe form:
//
//	cents = a*1e9 + b   (a = cents/1e9, b < 1e9)
//	out   = a*ppb + (b*ppb + 5e8)/1e9
//
// b*ppb < 1e9*1e9 = 1e18 always fits; a*ppb overflows only past ~1e28
// cents-by-rate — unreachable. A rate is a ratio, never money: ppb is
// parts-per-billion of the rate, NOT micro-cents (#462's separation from
// #461). Negative cents round half-away-from-zero (the float path's
// floor(x+0.5) differed by one cent at the exact -.5 edge — recorded).
func ConvertCentsPPB(cents int64, ppb int64) int64 {
	if ppb <= 0 {
		return cents
	}
	if cents < 0 {
		return -ConvertCentsPPB(-cents, ppb)
	}
	a, b := cents/1_000_000_000, cents%1_000_000_000
	return a*ppb + (b*ppb+500_000_000)/1_000_000_000
}

// PPBIdentity is the identity rate (1.0) in parts-per-billion.
const PPBIdentity int64 = 1_000_000_000
