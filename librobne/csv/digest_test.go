package csv

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestDigestRestartCountSum pins container restart accumulation in the daily
// digest: RestartCount values must SUM across samples (crash-loop quality gate).
func TestDigestRestartCountSum(t *testing.T) {
	bucket := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	rows := []Row{
		{IntervalStart: bucket.Add(time.Hour), RestartCount: 2, OOMCount: 100},
		{IntervalStart: bucket.Add(2 * time.Hour), RestartCount: 3, OOMCount: 200},
		{IntervalStart: bucket.Add(3 * time.Hour), RestartCount: 7, OOMCount: 300},
	}
	d := computeDay(bucket, rows)
	// SUM is 12: a MAX confusion would yield 7, a COUNT confusion 3.
	assert.Equal(t, int64(12), d.RestartCountSum)
	// OOM must keep its own lane: a restart/oom cross-wire would yield 600 here.
	assert.Equal(t, int64(600), d.OOMCountSum)
}
