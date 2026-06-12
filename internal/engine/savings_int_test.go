package engine

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRateMicroCentsPerMCHour(t *testing.T) {
	// $1/core-hour → 100_000 micro-cents/mc-hour
	assert.Equal(t, int64(100_000), RateMicroCentsPerMCHour(1.0))
	assert.Equal(t, int64(0), RateMicroCentsPerMCHour(0))
	assert.Equal(t, int64(0), RateMicroCentsPerMCHour(-1))
	// $0.007/core-hour
	assert.Equal(t, int64(700), RateMicroCentsPerMCHour(0.007))
}

func TestRateMicroCentsPerGiBHour(t *testing.T) {
	assert.Equal(t, int64(100_000_000), RateMicroCentsPerGiBHour(1.0))
	assert.Equal(t, int64(0), RateMicroCentsPerGiBHour(0))
}

func TestRateMicroCentsPerDollarMonth(t *testing.T) {
	assert.Equal(t, int64(600_000_000), RateMicroCentsPerDollarMonth(6.0))
}

func TestCPUSavingsMicroCents(t *testing.T) {
	// 300 mc delta, $1/core-hour, 730 hours, 1 replica → $219
	rate := RateMicroCentsPerMCHour(1.0)
	got := CPUSavingsMicroCents(300, rate, HoursPerMonthInt, 1)
	assert.Equal(t, int64(21_900_000_000), got)
	assert.InDelta(t, 219.0, MicroCentsToDollars(got), 0.01)
}

func TestMemSavingsMicroCentsFromKiB(t *testing.T) {
	// 1 GiB delta, $1/GiB-hour, 730 hours → $730
	deltaKiB := int64(1024 * 1024)
	rate := RateMicroCentsPerGiBHour(1.0)
	got := MemSavingsMicroCentsFromKiB(deltaKiB, rate, HoursPerMonthInt, 1)
	assert.InDelta(t, 730.0, MicroCentsToDollars(got), 0.01)
}

func TestGiBSavingsMicroCents(t *testing.T) {
	rate := RateMicroCentsPerGiBHour(0.02)
	got := GiBSavingsMicroCents(16, rate, HoursPerMonthInt)
	// 16 GiB * $0.02/GiB-hr * 730 = $233.60
	assert.InDelta(t, 233.60, MicroCentsToDollars(got), 0.01)
}

func TestVCPUSavingsMicroCents(t *testing.T) {
	rate := RateMicroCentsPerMCHour(0.6)
	got := VCPUSavingsMicroCents(4, rate, HoursPerMonthInt)
	// 4 vCPU * $0.6/core-hr * 730 = $1752
	assert.InDelta(t, 1752.0, MicroCentsToDollars(got), 0.01)
}

func TestStorageSavingsMicroCentsFromBytes(t *testing.T) {
	rate := RateMicroCentsPerGiBMonth(0.10)
	delta := int64(90 * 1024 * 1024 * 1024)
	got := StorageSavingsMicroCentsFromBytes(delta, rate)
	assert.InDelta(t, 9.0, MicroCentsToDollars(got), 0.01)
}

func TestMonthlyFlatSavingsMicroCents(t *testing.T) {
	rate := RateMicroCentsPerDollarMonth(1000)
	got := MonthlyFlatSavingsMicroCents(1, rate)
	assert.InDelta(t, 1000.0, MicroCentsToDollars(got), 0.01)
}

func TestMIGFractionSavingsMicroCents(t *testing.T) {
	rate := RateMicroCentsPerDollarMonth(700)
	got := MIGFractionSavingsMicroCents(rate, 7, 1)
	// (1 - 1/7) * 700 = 600
	assert.InDelta(t, 600.0, MicroCentsToDollars(got), 0.01)
}

func TestScaleMicroCentsByBasisPoints(t *testing.T) {
	base := int64(36_208_000_000) // $362.08 in micro-cents... wait that's wrong
	// $36208 in micro-cents = 36208 * 100_000_000
	base = 36208 * MicroCentsPerDollar
	got := ScaleMicroCentsByBasisPoints(base, 7000)
	assert.InDelta(t, 25345.6, MicroCentsToDollars(got), 0.01)
}

func TestMicroCentsToCents_Rounding(t *testing.T) {
	assert.Equal(t, int64(1), MicroCentsToCents(500_000))
	assert.Equal(t, int64(1), MicroCentsToCents(999_999))
	assert.Equal(t, int64(2), MicroCentsToCents(1_500_000))
	assert.Equal(t, int64(-1), MicroCentsToCents(-500_000))
	assert.Equal(t, int64(-2), MicroCentsToCents(-1_500_000))
}

func TestMicroCentsToDollars(t *testing.T) {
	assert.InDelta(t, 219.0, MicroCentsToDollars(21_900_000_000), 0.001)
}

func TestSavingsMicroCents_NegativeDelta(t *testing.T) {
	rate := RateMicroCentsPerMCHour(1.0)
	got := CPUSavingsMicroCents(-300, rate, HoursPerMonthInt, 1)
	assert.Less(t, got, int64(0))
	assert.InDelta(t, -219.0, MicroCentsToDollars(got), 0.01)
}

func TestSavingsMicroCents_ZeroInputs(t *testing.T) {
	rate := RateMicroCentsPerMCHour(1.0)
	assert.Equal(t, int64(0), CPUSavingsMicroCents(0, rate, HoursPerMonthInt, 1))
	assert.Equal(t, int64(0), CPUSavingsMicroCents(300, 0, HoursPerMonthInt, 1))
	assert.Equal(t, int64(0), CPUSavingsMicroCents(300, rate, 0, 1))
	assert.Equal(t, int64(0), CPUSavingsMicroCents(300, rate, HoursPerMonthInt, 0))
}

func TestMemorySavingsMicroCentsFromBytes_TwoGiB(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	rate := RateMicroCentsPerGiBHour(1.0)
	got := MemorySavingsMicroCentsFromBytes(2*gib, rate, HoursPerMonthInt)
	assert.Equal(t, int64(146000), MicroCentsToCents(got))
}

func TestSavingsMicroCents_LargeValuesNoOverflow(t *testing.T) {
	// Worst case from audit: 1M mc × 100M micro-cents/mc-hr × 730 hours
	rate := int64(100_000_000)
	delta := int64(1_000_000)
	got := CPUSavingsMicroCents(delta, rate, HoursPerMonthInt, 1)
	expected := int64(1_000_000) * int64(100_000_000) * HoursPerMonthInt
	assert.Equal(t, expected, got)
	assert.Less(t, float64(got), float64(math.MaxInt64)*0.9)
}

func TestEffectiveRateMicroCentsPerMCHour(t *testing.T) {
	// $730 cost / 730 core-hours = $1/core-hour
	got := EffectiveRateMicroCentsPerMCHour(730, 730)
	assert.Equal(t, RateMicroCentsPerMCHour(1.0), got)
	assert.Equal(t, int64(0), EffectiveRateMicroCentsPerMCHour(100, 0))
	// Negative cost clamped
	assert.Equal(t, int64(0), EffectiveRateMicroCentsPerMCHour(-500, 730))
}

func TestEffectiveRateMicroCentsPerGiBHour(t *testing.T) {
	got := EffectiveRateMicroCentsPerGiBHour(365, 730)
	assert.Equal(t, RateMicroCentsPerGiBHour(0.5), got)
}

func TestIntegerSavingsMatchesFloatPath_NodeDownsizing(t *testing.T) {
	// Reproduce node_savings_test downsizing scenario with integer path
	cpuRate := RateMicroCentsPerMCHour(0.01)
	memRate := RateMicroCentsPerGiBHour(0.02)
	nodeRate := RateMicroCentsPerDollarMonth(1000)

	cpuDelta := int64(4000) // 8000 - 4000 mc
	memDelta := int64(16 * KiBPerGiB)
	var total int64
	total += CPUSavingsMicroCents(cpuDelta, cpuRate, HoursPerMonthInt, 1)
	total += MemSavingsMicroCentsFromKiB(memDelta, memRate, HoursPerMonthInt, 1)
	total += MonthlyFlatSavingsMicroCents(1, nodeRate)

	assert.InDelta(t, 1262.80, MicroCentsToDollars(total), 0.01)
}
