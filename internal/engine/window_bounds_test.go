package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWindowBounds_ZeroCopySubslice(t *testing.T) {
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: base.AddDate(0, 0, 0)},
		{BucketDate: base.AddDate(0, 0, 1)},
		{BucketDate: base.AddDate(0, 0, 2)},
		{BucketDate: base.AddDate(0, 0, 3)},
		{BucketDate: base.AddDate(0, 0, 4)},
	}
	endDate := base.AddDate(0, 0, 4)

	lo, hi := windowBounds(rows, endDate, 3)
	window := rows[lo:hi]

	assert.Equal(t, 2, lo)
	assert.Equal(t, 5, hi)
	assert.Len(t, window, 3)
	assert.Equal(t, &rows[2], &window[0], "window must share backing array with source rows")
}

func TestWindowBounds_ExcludesRowsAfterEndDate(t *testing.T) {
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: base},
		{BucketDate: base.AddDate(0, 0, 1)},
		{BucketDate: base.AddDate(0, 0, 5)},
	}

	lo, hi := windowBounds(rows, base.AddDate(0, 0, 1), 7)
	assert.Equal(t, 0, lo)
	assert.Equal(t, 2, hi)
}

func TestWindowBounds_EmptyInput(t *testing.T) {
	lo, hi := windowBounds(nil, time.Now().UTC(), 7)
	assert.Equal(t, 0, lo)
	assert.Equal(t, 0, hi)
}
