package engine

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// applyContainerSavings maps ClusterCostData once (P1b mapper) then Apply*.
func applyContainerSavings(recs []ContainerRec, cd *costdata.ClusterCostData, hours int64) {
	ApplySavingsEstimates(recs, costdata.ClusterCostDataToRateCard(cd), hours)
}

func TestApplySavingsEstimates_NilCostData(t *testing.T) {
	recs := []ContainerRec{
		{Namespace: "ns1", CurrentCPURequestMC: 500, RecCPURequestMC: 200},
	}

	ApplySavingsEstimates(recs, nil, 730)

	assert.Nil(t, recs[0].EstimatedSavingsCents)
	assert.Nil(t, recs[0].EstimatedCPUSavingsCents)
	assert.Nil(t, recs[0].EstimatedMemSavingsCents)
	assert.Contains(t, recs[0].NotificationCodes, NotifNoCostData)
}

func TestApplySavingsEstimates_NoCostData_NoDuplicate(t *testing.T) {
	recs := []ContainerRec{
		{
			Namespace:         "ns1",
			NotificationCodes: []int16{NotifNoCostData},
		},
	}

	ApplySavingsEstimates(recs, nil, 730)

	count := 0
	for _, c := range recs[0].NotificationCodes {
		if c == NotifNoCostData {
			count++
		}
	}
	assert.Equal(t, 1, count, "NotifNoCostData should not be duplicated")
}

func TestApplySavingsEstimates_NamespaceNotFound(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces:       map[string]costdata.NamespaceCosts{},
	}
	recs := []ContainerRec{
		{Namespace: "missing-ns", CurrentCPURequestMC: 500, RecCPURequestMC: 200},
	}

	applyContainerSavings(recs, cd, 730)

	assert.Nil(t, recs[0].EstimatedSavingsCents)
	assert.Contains(t, recs[0].NotificationCodes, NotifNoCostData)
}

func TestApplySavingsEstimates_CostModelOnly(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 730.0, // $1/core-hour effective
				CostModelMemCost: 0,
				InfraCost:        0,
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:            "ns1",
			CurrentCPURequestMC:  500, // 0.5 cores
			RecCPURequestMC:      200, // 0.2 cores
			CurrentMemRequestKiB: 1048576,
			RecMemRequestKiB:     1048576,
			PodCountAvg:          1,
		},
	}

	applyContainerSavings(recs, cd, 730)

	// Delta: 0.3 cores * $1/core-hour * 730 hours * 1 replica = $219
	assert.InDelta(t, 219.0, money.CentsToUSD(*recs[0].EstimatedSavingsCents), 0.01)
}

func TestApplySavingsEstimates_WithInfraCosts_CPUDistribution(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 0,
				CostModelMemCost: 0,
				InfraCost:        730.0, // $1/core-hour of infra
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:            "ns1",
			CurrentCPURequestMC:  1000, // 1 core
			RecCPURequestMC:      500,  // 0.5 cores
			CurrentMemRequestKiB: 1048576,
			RecMemRequestKiB:     1048576,
			PodCountAvg:          2,
		},
	}

	applyContainerSavings(recs, cd, 730)

	// Infra savings: 0.5 cores * $1/core-hour * 730 hours * 2 replicas = $730
	assert.InDelta(t, 730.0, money.CentsToUSD(*recs[0].EstimatedSavingsCents), 0.01)
}

func TestApplySavingsEstimates_WithInfraCosts_MemoryDistribution(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "memory",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 0,
				CostModelMemCost: 0,
				InfraCost:        730.0,
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:            "ns1",
			CurrentCPURequestMC:  1000,
			RecCPURequestMC:      1000,
			CurrentMemRequestKiB: 2 * 1024 * 1024, // 2 GiB
			RecMemRequestKiB:     1 * 1024 * 1024, // 1 GiB
			PodCountAvg:          1,
		},
	}

	applyContainerSavings(recs, cd, 730)

	// Infra savings (memory dist): 1 GiB * $1/GiB-hour * 730 hours * 1 replica = $730
	assert.InDelta(t, 730.0, money.CentsToUSD(*recs[0].EstimatedSavingsCents), 0.01)
}

func TestApplySavingsEstimates_ZeroPodCount_DefaultsToOne(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 730.0,
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:           "ns1",
			CurrentCPURequestMC: 500,
			RecCPURequestMC:     200,
			PodCountAvg:         0, // Should default to 1
		},
	}

	applyContainerSavings(recs, cd, 730)

	// Delta: 0.3 cores * $1/core-hour * 730 hours * 1 replica = $219
	assert.InDelta(t, 219.0, money.CentsToUSD(*recs[0].EstimatedSavingsCents), 0.01)
}

func TestApplySavingsEstimates_NegativeSavings_Underprovisioned(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 730.0,
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:           "ns1",
			CurrentCPURequestMC: 200,
			RecCPURequestMC:     500,
			PodCountAvg:         1,
		},
	}

	applyContainerSavings(recs, cd, 730)

	// Negative: recommendation costs more (under-provisioned)
	require.NotNil(t, recs[0].EstimatedSavingsCents)
	assert.Less(t, *recs[0].EstimatedSavingsCents, int64(0))
}

func TestApplySavingsEstimates_CombinedCostModelAndInfraAndDistributed(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 730.0, // $1/core-hour cost model
				CostModelMemCost: 365.0, // $0.5/GiB-hour cost model
				InfraCost:        365.0, // $0.5/core-hour infra (cpu distribution)
				DistributedCost:  365.0, // $0.5/core-hour distributed platform overhead
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:            "ns1",
			CurrentCPURequestMC:  1000,            // 1 core
			RecCPURequestMC:      500,             // 0.5 cores
			CurrentMemRequestKiB: 2 * 1024 * 1024, // 2 GiB
			RecMemRequestKiB:     1 * 1024 * 1024, // 1 GiB
			PodCountAvg:          2,
		},
	}

	applyContainerSavings(recs, cd, 730)

	// Cost model savings:
	//   CPU: 0.5 cores * $1/core-hr * 730 hrs * 2 pods = $730
	//   MEM: 1 GiB * $0.5/GiB-hr * 730 hrs * 2 pods = $730
	// Infra+distributed savings (cpu distribution):
	//   (365+365)/730 = $1/core-hr
	//   0.5 cores * $1/core-hr * 730 hrs * 2 pods = $730
	// Total = 730 + 730 + 730 = $2190
	assert.InDelta(t, 2190.0, money.CentsToUSD(*recs[0].EstimatedSavingsCents), 0.01)
}

func TestApplySavingsEstimates_DistributedCostOnly(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 0,
				CostModelMemCost: 0,
				InfraCost:        0,
				DistributedCost:  730.0, // $1/core-hour from node/cluster monthly costs
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:           "ns1",
			CurrentCPURequestMC: 500,
			RecCPURequestMC:     200,
			PodCountAvg:         1,
		},
	}

	applyContainerSavings(recs, cd, 730)

	// 0.3 cores * $1/core-hour * 730 hours * 1 replica = $219
	assert.InDelta(t, 219.0, money.CentsToUSD(*recs[0].EstimatedSavingsCents), 0.01)
}

func TestApplySavingsEstimates_DistributedCost_MemoryDistribution(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "memory",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 0,
				CostModelMemCost: 0,
				InfraCost:        0,
				DistributedCost:  730.0,
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:            "ns1",
			CurrentCPURequestMC:  500,
			RecCPURequestMC:      500,
			CurrentMemRequestKiB: 2 * 1024 * 1024, // 2 GiB
			RecMemRequestKiB:     1 * 1024 * 1024, // 1 GiB
			PodCountAvg:          1,
		},
	}

	applyContainerSavings(recs, cd, 730)

	// memory distribution: 1 GiB * $1/GiB-hr * 730 hrs * 1 pod = $730
	assert.InDelta(t, 730.0, money.CentsToUSD(*recs[0].EstimatedSavingsCents), 0.01)
}

func TestApplySavingsEstimates_UsesDesiredReplicas(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 730.0, // $1/core-hour
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:           "ns1",
			CurrentCPURequestMC: 500, // 0.5 cores
			RecCPURequestMC:     200, // 0.2 cores
			PodCountAvg:         2,   // fallback
			DesiredReplicas:     5,   // authoritative - should be used
		},
	}

	applyContainerSavings(recs, cd, 730)

	// Delta: 0.3 cores * $1/core-hour * 730 hours * 5 replicas = $1095
	assert.InDelta(t, 1095.0, money.CentsToUSD(*recs[0].EstimatedSavingsCents), 0.01)
}

func TestApplySavingsEstimates_NegativeCostDataClamped(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: -500.0, // corrupted negative
				CostModelMemCost: -200.0,
				InfraCost:        -100.0,
				DistributedCost:  -50.0,
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:           "ns1",
			CurrentCPURequestMC: 1000,
			RecCPURequestMC:     500,
			PodCountAvg:         1,
		},
	}

	applyContainerSavings(recs, cd, 730)

	// Negative rates are clamped to 0, so savings should be $0
	require.NotNil(t, recs[0].EstimatedSavingsCents)
	assert.Equal(t, int64(0), *recs[0].EstimatedSavingsCents)
}

func TestApplySavingsEstimates_ZeroConfiguredRates(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_per_hour":  {Infrastructure: 0, Supplementary: 0},
			"memory_gb_per_hour": {Infrastructure: 0, Supplementary: 0},
		},
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 0,
				CostModelMemCost: 0,
				InfraCost:        0,
				DistributedCost:  0,
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:            "ns1",
			CurrentCPURequestMC:  1000,
			RecCPURequestMC:      500,
			CurrentMemRequestKiB: 2 * 1024 * 1024,
			RecMemRequestKiB:     1 * 1024 * 1024,
			PodCountAvg:          1,
		},
	}

	applyContainerSavings(recs, cd, 730)

	require.NotNil(t, recs[0].EstimatedSavingsCents)
	assert.Equal(t, int64(0), *recs[0].EstimatedSavingsCents,
		"zero configured rates should produce zero savings, not panic or NaN")
}

func TestApplySavingsEstimates_ZeroUsageHours(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 100.0,
				CPURequestHours:  0, // Zero usage hours
				MemRequestHours:  0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:           "ns1",
			CurrentCPURequestMC: 500,
			RecCPURequestMC:     200,
			PodCountAvg:         1,
		},
	}

	applyContainerSavings(recs, cd, 730)

	// safeDiv returns 0 when denominator is 0
	require.NotNil(t, recs[0].EstimatedSavingsCents)
	assert.Equal(t, int64(0), *recs[0].EstimatedSavingsCents)
}

func TestApplySavingsEstimates_EmptyRateCard(t *testing.T) {
	recs := []ContainerRec{
		{Namespace: "ns1", CurrentCPURequestMC: 500, RecCPURequestMC: 200},
	}
	ApplySavingsEstimates(recs, &RateCard{}, 730)
	assert.Nil(t, recs[0].EstimatedSavingsCents)
	assert.Contains(t, recs[0].NotificationCodes, NotifNoCostData)
}

func TestApplySavingsEstimates_TierA_UnitPrices(t *testing.T) {
	recs := []ContainerRec{
		{
			Namespace:           "ns1",
			CurrentCPURequestMC: 500,
			RecCPURequestMC:     200,
			PodCountAvg:         1,
		},
	}
	card := &RateCard{
		CPUMicroCentsPerCoreHour: 100_000_000, // $1 / core-hour
	}
	ApplySavingsEstimates(recs, card, 730)
	require.NotNil(t, recs[0].EstimatedSavingsCents)
	assert.InDelta(t, 219.0, money.CentsToUSD(*recs[0].EstimatedSavingsCents), 0.01)
}

func TestApplySavingsEstimates_TierA_AppliesMarkup(t *testing.T) {
	recs := []ContainerRec{
		{
			Namespace:           "ns1",
			CurrentCPURequestMC: 500,
			RecCPURequestMC:     200,
			PodCountAvg:         1,
		},
	}
	card := &RateCard{
		CPUMicroCentsPerCoreHour: 100_000_000,
		MarkupBasisPoints:        1000, // 10%
	}
	ApplySavingsEstimates(recs, card, 730)
	require.NotNil(t, recs[0].EstimatedSavingsCents)
	assert.InDelta(t, 240.90, money.CentsToUSD(*recs[0].EstimatedSavingsCents), 0.01)
}

func TestApplySavingsEstimates_TierAB_PrefersNamespaceSpend(t *testing.T) {
	recs := []ContainerRec{
		{
			Namespace:           "ns1",
			CurrentCPURequestMC: 500,
			RecCPURequestMC:     200,
			PodCountAvg:         1,
		},
	}
	card := &RateCard{
		CPUMicroCentsPerCoreHour: 10_000_000_000, // would dwarf B if used
		Namespaces: map[string]NamespaceSpend{
			"ns1": {
				CostModelCPUMicroCents: 730 * 100_000_000,
				CPURequestMilliHours:   730_000,
			},
		},
	}
	ApplySavingsEstimates(recs, card, 730)
	require.NotNil(t, recs[0].EstimatedSavingsCents)
	assert.InDelta(t, 219.0, money.CentsToUSD(*recs[0].EstimatedSavingsCents), 0.01)
}

func TestApplySavingsEstimates_TierB_MissingNamespace_NoAFallback(t *testing.T) {
	recs := []ContainerRec{
		{Namespace: "missing", CurrentCPURequestMC: 500, RecCPURequestMC: 200, PodCountAvg: 1},
	}
	card := &RateCard{
		CPUMicroCentsPerCoreHour: 100_000_000,
		Namespaces: map[string]NamespaceSpend{
			"other": {CostModelCPUMicroCents: 730 * 100_000_000, CPURequestMilliHours: 730_000},
		},
	}
	ApplySavingsEstimates(recs, card, 730)
	assert.Nil(t, recs[0].EstimatedSavingsCents)
	assert.Contains(t, recs[0].NotificationCodes, NotifNoCostData)
}

func TestApplySavingsEstimates_IdleUsesFullCurrentRequest(t *testing.T) {
	recs := []ContainerRec{
		{
			Namespace:           "ns1",
			CurrentCPURequestMC: 500,
			RecCPURequestMC:     200,
			PodCountAvg:         1,
			IdleState:           IdleStateIdle,
		},
	}
	card := &RateCard{
		Namespaces: map[string]NamespaceSpend{
			"ns1": {
				CostModelCPUMicroCents: 730 * 100_000_000,
				CPURequestMilliHours:   730_000,
			},
		},
	}
	ApplySavingsEstimates(recs, card, 730)
	require.NotNil(t, recs[0].EstimatedSavingsCents)
	require.NotNil(t, recs[0].EstimatedWasteCents)
	assert.Equal(t, *recs[0].EstimatedSavingsCents, *recs[0].EstimatedWasteCents)
	// idle: full 0.5 cores × $1 × 730h = $365, not the 0.3-core delta
	assert.InDelta(t, 365.0, money.CentsToUSD(*recs[0].EstimatedSavingsCents), 0.01)
}
