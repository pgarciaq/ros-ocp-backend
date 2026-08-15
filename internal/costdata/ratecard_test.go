package costdata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterCostDataToRateCard_Nil(t *testing.T) {
	assert.Nil(t, ClusterCostDataToRateCard(nil))
}

func TestClusterCostDataToRateCard_CopiesCurrencyAsIs(t *testing.T) {
	empty := ClusterCostDataToRateCard(&ClusterCostData{})
	require.NotNil(t, empty)
	assert.Equal(t, "", empty.Currency, "empty currency must stay unset, not USD")
	assert.True(t, empty.IsEmpty())

	eur := ClusterCostDataToRateCard(&ClusterCostData{Currency: "EUR"})
	assert.Equal(t, "EUR", eur.Currency)
}

func TestClusterCostDataToRateCard_DoesNotCopyMarkupOrConfiguredRates(t *testing.T) {
	card := ClusterCostDataToRateCard(&ClusterCostData{
		MarkupPct: 10,
		ConfiguredRates: map[string]RatePair{
			"cpu_core_request_per_hour": {Supplementary: 0.2},
		},
		Namespaces: map[string]NamespaceCosts{
			"ns1": {CostModelCPUCost: 730, CPURequestHours: 730},
		},
	})
	require.NotNil(t, card)
	assert.Equal(t, int64(0), card.MarkupBasisPoints)
	assert.Equal(t, int64(0), card.CPUMicroCentsPerCoreHour)
	require.Contains(t, card.Namespaces, "ns1")
	assert.Equal(t, int64(730*100_000_000), card.Namespaces["ns1"].CostModelCPUMicroCents)
	assert.Equal(t, int64(730_000), card.Namespaces["ns1"].CPURequestMilliHours)
}

func TestClusterCostDataToRateCard_ClampsNegativesAndRoundsHours(t *testing.T) {
	card := ClusterCostDataToRateCard(&ClusterCostData{
		Namespaces: map[string]NamespaceCosts{
			"ns1": {
				CostModelCPUCost: -5,
				CostModelMemCost: 1.5,
				InfraCost:        -1,
				CPURequestHours:  729.6,
				MemRequestHours:  0,
			},
		},
	})
	ns := card.Namespaces["ns1"]
	assert.Equal(t, int64(0), ns.CostModelCPUMicroCents)
	assert.Equal(t, int64(0), ns.InfraMicroCents)
	assert.Equal(t, int64(150_000_000), ns.CostModelMemMicroCents)
	assert.Equal(t, int64(729_600), ns.CPURequestMilliHours)
	assert.Equal(t, int64(0), ns.MemRequestMilliHours)
}

func TestClusterCostDataToRateCard_EmptyNamespacesMapIsTierB(t *testing.T) {
	card := ClusterCostDataToRateCard(&ClusterCostData{
		Namespaces: map[string]NamespaceCosts{},
	})
	require.NotNil(t, card)
	assert.True(t, card.HasTierB())
	assert.True(t, card.IsEmpty())
}
