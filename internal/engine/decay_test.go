package engine

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDecayWeight_ZeroAge_FullWeight(t *testing.T) {
	w := DecayWeight(0, 168)
	assert.InDelta(t, 1.0, w, 0.001)
}

func TestDecayWeight_OneHalfLife_HalfWeight(t *testing.T) {
	w := DecayWeight(168, 168) // 1 half-life
	assert.InDelta(t, 0.5, w, 0.001)
}

func TestDecayWeight_TwoHalfLives_QuarterWeight(t *testing.T) {
	w := DecayWeight(336, 168)
	assert.InDelta(t, 0.25, w, 0.001)
}

func TestDecayWeight_ZeroHalfLife_ReturnsOne(t *testing.T) {
	w := DecayWeight(100, 0)
	assert.InDelta(t, 1.0, w, 0.001)
}

func TestWeightedPercentile_RecentDataWeightedMore(t *testing.T) {
	now := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now.AddDate(0, 0, -6), CPUUsageP60MC: 100}, // 6 days old
		{BucketDate: now.AddDate(0, 0, -5), CPUUsageP60MC: 200},
		{BucketDate: now.AddDate(0, 0, -4), CPUUsageP60MC: 200},
		{BucketDate: now.AddDate(0, 0, -3), CPUUsageP60MC: 200},
		{BucketDate: now.AddDate(0, 0, -2), CPUUsageP60MC: 200},
		{BucketDate: now.AddDate(0, 0, -1), CPUUsageP60MC: 200},
		{BucketDate: now, CPUUsageP60MC: 200},
	}
	result := WeightedPercentile(rows, now, 72, func(r DigestRow) int64 { return r.CPUUsageP60MC })
	// With 72h half-life, the old 100mc value (6 days = 144h ≈ 2 half-lives, weight ~0.25)
	// is heavily downweighted. Result should be much closer to 200 than 100.
	assert.Greater(t, result, int64(180))
}

func TestWeightedPercentile_NoDecay_ShortTerm(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, CPUUsageP60MC: 100},
	}
	// halflife=0 means no decay (all weights 1.0)
	result := WeightedPercentile(rows, now, 0, func(r DigestRow) int64 { return r.CPUUsageP60MC })
	assert.Equal(t, int64(100), result)
}

func TestWeightedPercentile_OldDataAlmostIgnored(t *testing.T) {
	now := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now.AddDate(0, 0, -14), CPUUsageP60MC: 1000}, // 14 days old
		{BucketDate: now, CPUUsageP60MC: 100},                     // today
	}
	// 72h half-life, 14 days = 336h ≈ 4.7 half-lives → weight ~0.038
	result := WeightedPercentile(rows, now, 72, func(r DigestRow) int64 { return r.CPUUsageP60MC })
	assert.Less(t, result, int64(150))
}

func TestWeightedPercentile_Empty_ReturnsZero(t *testing.T) {
	now := time.Now()
	result := WeightedPercentile(nil, now, 168, func(r DigestRow) int64 { return r.CPUUsageP60MC })
	assert.Equal(t, int64(0), result)
}

func TestDecayWeight_TableLookup_MatchesExp(t *testing.T) {
	halfLives := []float64{12, 24, 72, 168, 360, 720}
	for _, hl := range halfLives {
		maxAge := int(hl * 2)
		for age := 0; age <= maxAge; age++ {
			got := DecayWeight(float64(age), hl)
			want := math.Exp(-float64(age) * math.Ln2 / hl)
			assert.InDelta(t, want, got, 0.0001, "age=%d halfLife=%v", age, hl)
		}
	}
}

func TestDecayWeight_TableLookup_RoundsFractionalAge(t *testing.T) {
	// 47.4h rounds to 47h for lookup; compare against the rounded-age value.
	got := DecayWeight(47.4, 72)
	want := math.Exp(-47 * math.Ln2 / 72)
	assert.InDelta(t, want, got, 0.0001)
}

func TestDecayWeight_TableLookup_CustomHalfLife(t *testing.T) {
	// Non-standard integer half-life builds a table on first use.
	hl := 200.0
	got := DecayWeight(100, hl)
	want := math.Exp(-100 * math.Ln2 / hl)
	assert.InDelta(t, want, got, 0.003)

	// Second call reuses the cached table.
	got2 := DecayWeight(50, hl)
	want2 := math.Exp(-50 * math.Ln2 / hl)
	assert.InDelta(t, want2, got2, 0.003)
}

func TestDecayWeight_NonIntegerHalfLife_FallsBackToExp(t *testing.T) {
	got := DecayWeight(48.5, 167.3)
	want := math.Exp(-48.5 * math.Ln2 / 167.3)
	assert.InDelta(t, want, got, 0.0001)
}

func TestDecayWeight_BeyondWindow_ReturnsZero(t *testing.T) {
	// maxAge = halfLife*2; age beyond that returns 0.
	w := DecayWeight(400, 168)
	assert.Equal(t, 0.0, w)
}

func TestDeriveDecayHalfLifeHours(t *testing.T) {
	assert.InDelta(t, 360.0, DeriveDecayHalfLifeHours(30), 0.001)
	assert.InDelta(t, 168.0, DeriveDecayHalfLifeHours(14), 0.001)
	assert.InDelta(t, 12.0, DeriveDecayHalfLifeHours(1), 0.001)
}
