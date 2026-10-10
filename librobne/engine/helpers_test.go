package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSumRestartCounts pins the crash-loop quality-gate helper: RestartCountSum
// values must SUM across digest rows in the term window.
func TestSumRestartCounts(t *testing.T) {
	// SUM is 12: a MAX confusion would yield 7, a first-row confusion 2.
	rows := []DigestRow{
		{RestartCountSum: 2},
		{RestartCountSum: 3},
		{RestartCountSum: 7},
	}
	assert.Equal(t, int64(12), SumRestartCounts(rows))
	// Empty window sums to 0 (no crash loops observed), it must not panic.
	assert.Equal(t, int64(0), SumRestartCounts(nil))
}
