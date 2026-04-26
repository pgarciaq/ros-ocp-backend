package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAggregatePodCounts(t *testing.T) {
	t.Run("empty rows returns zeros", func(t *testing.T) {
		mn, mx, avg := aggregatePodCounts(nil)
		assert.Equal(t, int64(0), mn)
		assert.Equal(t, int64(0), mx)
		assert.Equal(t, int64(0), avg)
	})

	t.Run("all zeros returns zeros", func(t *testing.T) {
		rows := []DigestRow{
			{PodCountMin: 0, PodCountMax: 0, PodCountAvg: 0},
			{PodCountMin: 0, PodCountMax: 0, PodCountAvg: 0},
		}
		mn, mx, avg := aggregatePodCounts(rows)
		assert.Equal(t, int64(0), mn)
		assert.Equal(t, int64(0), mx)
		assert.Equal(t, int64(0), avg)
	})

	t.Run("single row propagates values", func(t *testing.T) {
		rows := []DigestRow{
			{PodCountMin: 2, PodCountMax: 5, PodCountAvg: 3},
		}
		mn, mx, avg := aggregatePodCounts(rows)
		assert.Equal(t, int64(2), mn)
		assert.Equal(t, int64(5), mx)
		assert.Equal(t, int64(3), avg)
	})

	t.Run("multiple rows takes min of mins and max of maxes", func(t *testing.T) {
		rows := []DigestRow{
			{PodCountMin: 2, PodCountMax: 4, PodCountAvg: 3},
			{PodCountMin: 1, PodCountMax: 6, PodCountAvg: 4},
			{PodCountMin: 3, PodCountMax: 5, PodCountAvg: 4},
		}
		mn, mx, avg := aggregatePodCounts(rows)
		assert.Equal(t, int64(1), mn)
		assert.Equal(t, int64(6), mx)
		assert.Equal(t, int64(4), avg) // round((3+4+4)/3) = round(3.667) = 4
	})

	t.Run("skips zero rows in aggregation", func(t *testing.T) {
		rows := []DigestRow{
			{PodCountMin: 2, PodCountMax: 4, PodCountAvg: 3},
			{PodCountMin: 0, PodCountMax: 0, PodCountAvg: 0},
			{PodCountMin: 1, PodCountMax: 3, PodCountAvg: 2},
		}
		mn, mx, avg := aggregatePodCounts(rows)
		assert.Equal(t, int64(1), mn)
		assert.Equal(t, int64(4), mx)
		assert.Equal(t, int64(3), avg) // round((3+2)/2) = 3 (2.5 rounds to 3 with math.Round)
	})

	t.Run("avg rounds correctly", func(t *testing.T) {
		rows := []DigestRow{
			{PodCountMin: 1, PodCountMax: 3, PodCountAvg: 2},
			{PodCountMin: 1, PodCountMax: 3, PodCountAvg: 3},
		}
		mn, mx, avg := aggregatePodCounts(rows)
		assert.Equal(t, int64(1), mn)
		assert.Equal(t, int64(3), mx)
		assert.Equal(t, int64(3), avg) // round((2+3)/2) = round(2.5) = 3 (math.Round rounds .5 up for odd)
	})
}
