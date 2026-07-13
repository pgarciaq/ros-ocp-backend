package core

import (
	"errors"
	"math"
	"strings"
	"time"
)

// WithinTolerance returns true if actual is within pct (0.05 = 5%) of expected.
func WithinTolerance(actual, expected int64, pct float64) bool {
	if expected == 0 {
		return actual == 0
	}
	delta := math.Abs(float64(actual)-float64(expected)) / float64(expected)
	return delta <= pct
}

// ComputeRecommendationAgeHours returns truncated integer hours since updatedAt.
// Returns 0 if updatedAt is zero or in the future (clock skew).
func ComputeRecommendationAgeHours(updatedAt time.Time, now time.Time) int64 {
	if updatedAt.IsZero() {
		return 0
	}
	hours := int64(now.Sub(updatedAt).Hours())
	if hours < 0 {
		return 0
	}
	return hours
}

// IsPartitionMissing detects "no partition" database errors.
func IsPartitionMissing(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrPartitionMissing) || strings.Contains(err.Error(), "no partition")
}

// MaxWindowDays returns the largest WindowDays across the given terms,
// with a floor of minFloor (use 0 for no floor).
func MaxWindowDays(terms []TermConfig, minFloor int) int {
	max := minFloor
	for _, tc := range terms {
		if tc.WindowDays > max {
			max = tc.WindowDays
		}
	}
	return max
}
