package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHoursInMonth_KnownValues(t *testing.T) {
	tests := []struct {
		year  int
		month time.Month
		want  int64
	}{
		{2026, time.January, 31 * 24},
		{2026, time.February, 28 * 24},
		{2024, time.February, 29 * 24}, // leap year
		{2026, time.March, 31 * 24},
		{2026, time.April, 30 * 24},
		{2026, time.June, 30 * 24},
		{2026, time.July, 31 * 24},
		{2026, time.September, 30 * 24},
		{2026, time.December, 31 * 24},
	}
	for _, tc := range tests {
		t.Run(tc.month.String(), func(t *testing.T) {
			got := HoursInMonth(tc.year, tc.month)
			assert.Equal(t, tc.want, got, "HoursInMonth(%d, %s)", tc.year, tc.month)
		})
	}
}

func TestHoursInMonth_LeapYear(t *testing.T) {
	assert.Equal(t, int64(29*24), HoursInMonth(2024, time.February))
	assert.Equal(t, int64(28*24), HoursInMonth(2025, time.February))
	assert.Equal(t, int64(29*24), HoursInMonth(2028, time.February))
}

func TestHoursInMonth_AllMonths2026(t *testing.T) {
	expected := []int64{
		744, 672, 744, 720, 744, 720, 744, 744, 720, 744, 720, 744,
	}
	for m := time.January; m <= time.December; m++ {
		got := HoursInMonth(2026, m)
		assert.Equal(t, expected[m-1], got, "month %s", m)
	}
}

func TestHoursInMonth_DiffersFrom730(t *testing.T) {
	differs := false
	for m := time.January; m <= time.December; m++ {
		if HoursInMonth(2026, m) != 730 {
			differs = true
			break
		}
	}
	require.True(t, differs, "HoursInMonth should differ from 730 for at least one month")
}

func TestCPUSavingsMicroCents_CalendarAccurate(t *testing.T) {
	deltaMC := int64(1000)
	rate := int64(100_000) // micro-cents/mc-hour
	replicas := int64(1)

	jan := CPUSavingsMicroCents(deltaMC, rate, HoursInMonth(2026, time.January), replicas)
	feb := CPUSavingsMicroCents(deltaMC, rate, HoursInMonth(2026, time.February), replicas)
	apr := CPUSavingsMicroCents(deltaMC, rate, HoursInMonth(2026, time.April), replicas)

	assert.Greater(t, jan, feb, "January (744h) should yield more savings than February (672h)")
	assert.Greater(t, jan, apr, "January (744h) should yield more savings than April (720h)")
	assert.Greater(t, apr, feb, "April (720h) should yield more savings than February (672h)")
}

func TestMemSavingsMicroCentsFromKiB_CalendarAccurate(t *testing.T) {
	deltaKiB := int64(KiBPerGiB) // 1 GiB
	rate := int64(100_000_000)   // micro-cents/GiB-hour
	replicas := int64(1)

	jul := MemSavingsMicroCentsFromKiB(deltaKiB, rate, HoursInMonth(2026, time.July), replicas)
	feb := MemSavingsMicroCentsFromKiB(deltaKiB, rate, HoursInMonth(2026, time.February), replicas)

	assert.Greater(t, jul, feb, "July (744h) should yield more savings than February (672h)")

	ratio := float64(jul) / float64(feb)
	expectedRatio := float64(744) / float64(672)
	assert.InDelta(t, expectedRatio, ratio, 0.001)
}

func TestQuotaTightenSavingsMicroCents_CalendarAccurate(t *testing.T) {
	cpuDelta := int64(500)
	memDelta := int64(BytesPerGiB)
	storDelta := int64(0)
	cpuRate := int64(100_000)
	memRate := int64(100_000_000)
	storRate := int64(0)

	savings730 := QuotaTightenSavingsMicroCents(cpuDelta, memDelta, storDelta, cpuRate, memRate, storRate, 730)
	savings744 := QuotaTightenSavingsMicroCents(cpuDelta, memDelta, storDelta, cpuRate, memRate, storRate, 744)
	savings672 := QuotaTightenSavingsMicroCents(cpuDelta, memDelta, storDelta, cpuRate, memRate, storRate, 672)

	assert.Greater(t, savings744, savings730, "744h should yield more than 730h")
	assert.Greater(t, savings730, savings672, "730h should yield more than 672h")
}
