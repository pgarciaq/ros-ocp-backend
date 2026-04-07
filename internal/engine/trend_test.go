package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestComputeTrendSlope_IncreasingUsage(t *testing.T) {
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]DigestRow, 7)
	for i := range rows {
		rows[i] = DigestRow{
			BucketDate:    base.AddDate(0, 0, i),
			CPUUsageP98MC: int64(100 + i*20), // 100, 120, 140, ..., 220
		}
	}
	slope := ComputeTrendSlope(rows, func(r DigestRow) int64 { return r.CPUUsageP98MC })
	assert.Greater(t, slope, 0.0)
	assert.InDelta(t, 20.0, slope, 0.1) // +20mc per day
}

func TestComputeTrendSlope_StableUsage(t *testing.T) {
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]DigestRow, 7)
	for i := range rows {
		rows[i] = DigestRow{
			BucketDate:    base.AddDate(0, 0, i),
			CPUUsageP98MC: 100,
		}
	}
	slope := ComputeTrendSlope(rows, func(r DigestRow) int64 { return r.CPUUsageP98MC })
	assert.InDelta(t, 0.0, slope, 0.01)
}

func TestComputeTrendSlope_DecreasingUsage(t *testing.T) {
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]DigestRow, 7)
	for i := range rows {
		rows[i] = DigestRow{
			BucketDate:    base.AddDate(0, 0, i),
			CPUUsageP98MC: int64(200 - i*10),
		}
	}
	slope := ComputeTrendSlope(rows, func(r DigestRow) int64 { return r.CPUUsageP98MC })
	assert.Less(t, slope, 0.0)
	assert.InDelta(t, -10.0, slope, 0.1)
}

func TestComputeTrendSlope_SinglePoint_ReturnsZero(t *testing.T) {
	rows := []DigestRow{{BucketDate: time.Now(), CPUUsageP98MC: 100}}
	slope := ComputeTrendSlope(rows, func(r DigestRow) int64 { return r.CPUUsageP98MC })
	assert.Equal(t, 0.0, slope)
}

func TestComputeTrendSlope_Empty_ReturnsZero(t *testing.T) {
	slope := ComputeTrendSlope(nil, func(r DigestRow) int64 { return r.CPUUsageP98MC })
	assert.Equal(t, 0.0, slope)
}
