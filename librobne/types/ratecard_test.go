package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRateCard_IsEmpty(t *testing.T) {
	assert.True(t, (*RateCard)(nil).IsEmpty())
	assert.True(t, (&RateCard{}).IsEmpty())
	assert.True(t, (&RateCard{Currency: "EUR"}).IsEmpty(), "currency alone is not a priced card")
	assert.False(t, (&RateCard{CPUMicroCentsPerCoreHour: 1}).IsEmpty())
	assert.False(t, (&RateCard{Namespaces: map[string]NamespaceSpend{"ns": {}}}).IsEmpty())
}

func TestRateCard_HasTierB(t *testing.T) {
	assert.False(t, (*RateCard)(nil).HasTierB())
	assert.False(t, (&RateCard{}).HasTierB())
	assert.True(t, (&RateCard{Namespaces: map[string]NamespaceSpend{}}).HasTierB())
}

func TestRateCard_TierAMarkup(t *testing.T) {
	card := &RateCard{CPUMicroCentsPerCoreHour: 100_000_000, MarkupBasisPoints: 1000}
	assert.Equal(t, int64(110_000), card.CPURateMicroCentsPerMCHour())
	noMarkup := &RateCard{CPUMicroCentsPerCoreHour: 100_000_000}
	assert.Equal(t, int64(100_000), noMarkup.CPURateMicroCentsPerMCHour())
}

func TestEffectiveRateFromTotals_MatchesKnownUSDFixtures(t *testing.T) {
	// $730 / 730 core-hours → $1/core-hour → 100_000 µ¢ / mc-hour
	assert.Equal(t, int64(100_000), EffectiveRateFromCPUTotals(730*100_000_000, 730_000))
	// $365 / 730 GiB-hours → $0.50/GiB-hour → 50_000_000 µ¢ / GiB-hour
	assert.Equal(t, int64(50_000_000), EffectiveRateFromMemTotals(365*100_000_000, 730_000))
	assert.Equal(t, int64(0), EffectiveRateFromCPUTotals(100, 0))
	assert.Equal(t, int64(0), EffectiveRateFromCPUTotals(-1, 730_000))
}
