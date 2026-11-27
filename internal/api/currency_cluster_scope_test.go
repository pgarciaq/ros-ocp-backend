package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

// #954(b): currency is a per-cluster property (koku's effective_rates.currency
// is the provider's cost-model currency), and the request-scoped cache used to
// memoize one scalar for the whole request. Every cluster after the first was
// therefore resolved to the first cluster's currency, so its amounts were
// relabelled without conversion. The GPU list installs that cache
// (handlers_node_recs.go), which is where the mixed-denomination total would
// have come from.
func TestGetCachedCurrencyIsPerCluster(t *testing.T) {
	// GetCachedCostRates only consults the cache when a cost provider exists,
	// so enable one (no HTTP happens: the per-cluster entries are pre-seeded).
	// Cleanup is registered before t.Setenv so it runs after the env restore.
	t.Cleanup(func() {
		ResetGPUCostProviderForTest()
		config.ResetForTest()
	})
	t.Setenv("KOKU_MASU_URL", "http://masu.example:5042")
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	config.ResetForTest()

	ctx := WithEnrichmentCache(context.Background(), "org1234567")
	cache := enrichmentCacheFromContext(ctx)
	require.NotNil(t, cache, "the test needs the request-scoped cache the GPU list installs")

	// Distinct currencies, seeded through the same per-cluster map
	// GetCachedCostRates fills, so the assertion fails on the collapse rather
	// than on a stubbed resolver.
	cache.costRates["cluster-a"] = &costdata.ClusterCostData{ClusterID: "cluster-a", Currency: "USD"}
	cache.costRates["cluster-b"] = &costdata.ClusterCostData{ClusterID: "cluster-b", Currency: "EUR"}

	assert.Equal(t, "USD", GetCachedCurrency(ctx, "org1234567", "cluster-a"))
	assert.Equal(t, "EUR", GetCachedCurrency(ctx, "org1234567", "cluster-b"),
		"the second cluster keeps its own cost-model currency; a request-wide scalar relabels it")
}

// The page total must be commensurable: only rows already in the page currency
// contribute, and the exclusion is named. Values are distinct and the foreign
// row sits between the included ones, so a first-N-rows or raw-sum bug cannot
// pass by coincidence (raw sum here is 30.00, not 15.50).
func TestSumNodeGPUSavingsExcludesForeignDenominations(t *testing.T) {
	recs := []model.NodeGPURecommendation{
		{EstimatedMonthlySavings: &money.MoneyAmount{Value: "10.00", Units: "USD"}},
		{EstimatedMonthlySavings: &money.MoneyAmount{Value: "20.00", Units: "EUR"}},
		{EstimatedMonthlySavings: nil},
		{EstimatedMonthlySavings: &money.MoneyAmount{Value: "5.50", Units: "USD"}},
		{EstimatedMonthlySavings: &money.MoneyAmount{Value: "1.00", Units: "EUR"}},
	}

	total, warnings := sumNodeGPUSavings(recs, "USD")

	require.NotNil(t, total, "two USD rows contribute, so the page keeps a total")
	assert.Equal(t, "15.50", total.Value, "20.00 EUR and 1.00 EUR must not be added as USD")
	assert.Equal(t, "USD", total.Units)

	require.Len(t, warnings, 1, "one warning naming the excluded denomination")
	assert.Contains(t, warnings[0], "2 recommendations")
	assert.Contains(t, warnings[0], "EUR")
	assert.Contains(t, warnings[0], "USD")
}

// A page where nothing is in the page currency has no commensurable total: nil,
// plus the warning that says why. Reporting 0.00 would claim a converted sum.
func TestSumNodeGPUSavingsAllForeignYieldsNilTotal(t *testing.T) {
	recs := []model.NodeGPURecommendation{
		{EstimatedMonthlySavings: &money.MoneyAmount{Value: "20.00", Units: "EUR"}},
		{EstimatedMonthlySavings: &money.MoneyAmount{Value: "7.25", Units: "GBP"}},
	}

	total, warnings := sumNodeGPUSavings(recs, "USD")

	assert.Nil(t, total, "no row is denominated in the page currency")
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "2 recommendations")
	assert.Contains(t, warnings[0], "1 in EUR, 1 in GBP", "currencies are listed deterministically")
}

// Single-currency pages are unchanged: same total, no warning.
func TestSumNodeGPUSavingsSingleCurrencyHasNoWarning(t *testing.T) {
	recs := []model.NodeGPURecommendation{
		{EstimatedMonthlySavings: &money.MoneyAmount{Value: "10.00", Units: "USD"}},
		{EstimatedMonthlySavings: &money.MoneyAmount{Value: "5.50", Units: "USD"}},
	}

	total, warnings := sumNodeGPUSavings(recs, "USD")

	require.NotNil(t, total)
	assert.Equal(t, "15.50", total.Value)
	assert.Empty(t, warnings)
}
