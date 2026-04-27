package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

func TestApplySavingsEstimates_NilCostData(t *testing.T) {
	recs := []ContainerRec{
		{Namespace: "ns1", CurrentCPURequestMC: 500, RecCPURequestMC: 200},
	}

	ApplySavingsEstimates(recs, nil)

	assert.Equal(t, float32(0), recs[0].EstimatedSavingsUSD)
	assert.Contains(t, recs[0].NotificationCodes, NotifNoCostData)
}

func TestApplySavingsEstimates_NoCostData_NoDuplicate(t *testing.T) {
	recs := []ContainerRec{
		{
			Namespace:         "ns1",
			NotificationCodes: []int16{NotifNoCostData},
		},
	}

	ApplySavingsEstimates(recs, nil)

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

	ApplySavingsEstimates(recs, cd)

	assert.Equal(t, float32(0), recs[0].EstimatedSavingsUSD)
	assert.Contains(t, recs[0].NotificationCodes, NotifNoCostData)
}

func TestApplySavingsEstimates_CostModelOnly(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 730.0,  // $1/core-hour effective
				CostModelMemCost: 0,
				InfraCost:        0,
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:           "ns1",
			CurrentCPURequestMC: 500,  // 0.5 cores
			RecCPURequestMC:     200,  // 0.2 cores
			CurrentMemRequestKiB: 1048576,
			RecMemRequestKiB:     1048576,
			PodCountAvg:         1,
		},
	}

	ApplySavingsEstimates(recs, cd)

	// Delta: 0.3 cores * $1/core-hour * 730 hours * 1 replica = $219
	assert.InDelta(t, 219.0, float64(recs[0].EstimatedSavingsUSD), 1.0)
}

func TestApplySavingsEstimates_WithInfraCosts_CPUDistribution(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 0,
				CostModelMemCost: 0,
				InfraCost:        730.0,  // $1/core-hour of infra
				CPURequestHours:  730.0,
				MemRequestHours:  730.0,
			},
		},
	}
	recs := []ContainerRec{
		{
			Namespace:           "ns1",
			CurrentCPURequestMC: 1000,  // 1 core
			RecCPURequestMC:     500,   // 0.5 cores
			CurrentMemRequestKiB: 1048576,
			RecMemRequestKiB:     1048576,
			PodCountAvg:         2,
		},
	}

	ApplySavingsEstimates(recs, cd)

	// Infra savings: 0.5 cores * $1/core-hour * 730 hours * 2 replicas = $730
	assert.InDelta(t, 730.0, float64(recs[0].EstimatedSavingsUSD), 1.0)
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
			RecMemRequestKiB:     1 * 1024 * 1024,  // 1 GiB
			PodCountAvg:          1,
		},
	}

	ApplySavingsEstimates(recs, cd)

	// Infra savings (memory dist): 1 GiB * $1/GiB-hour * 730 hours * 1 replica = $730
	assert.InDelta(t, 730.0, float64(recs[0].EstimatedSavingsUSD), 1.0)
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

	ApplySavingsEstimates(recs, cd)

	// Delta: 0.3 cores * $1/core-hour * 730 hours * 1 replica = $219
	assert.InDelta(t, 219.0, float64(recs[0].EstimatedSavingsUSD), 1.0)
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

	ApplySavingsEstimates(recs, cd)

	// Negative: recommendation costs more (under-provisioned)
	assert.True(t, recs[0].EstimatedSavingsUSD < 0)
}

func TestApplySavingsEstimates_CombinedCostModelAndInfraAndDistributed(t *testing.T) {
	cd := &costdata.ClusterCostData{
		DistributionType: "cpu",
		Namespaces: map[string]costdata.NamespaceCosts{
			"ns1": {
				CostModelCPUCost: 730.0,  // $1/core-hour cost model
				CostModelMemCost: 365.0,  // $0.5/GiB-hour cost model
				InfraCost:        365.0,  // $0.5/core-hour infra (cpu distribution)
				DistributedCost:  365.0,  // $0.5/core-hour distributed platform overhead
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
			CurrentMemRequestKiB: 2 * 1024 * 1024, // 2 GiB
			RecMemRequestKiB:     1 * 1024 * 1024,  // 1 GiB
			PodCountAvg:          2,
		},
	}

	ApplySavingsEstimates(recs, cd)

	// Cost model savings:
	//   CPU: 0.5 cores * $1/core-hr * 730 hrs * 2 pods = $730
	//   MEM: 1 GiB * $0.5/GiB-hr * 730 hrs * 2 pods = $730
	// Infra+distributed savings (cpu distribution):
	//   (365+365)/730 = $1/core-hr
	//   0.5 cores * $1/core-hr * 730 hrs * 2 pods = $730
	// Total = 730 + 730 + 730 = $2190
	assert.InDelta(t, 2190.0, float64(recs[0].EstimatedSavingsUSD), 1.0)
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

	ApplySavingsEstimates(recs, cd)

	// 0.3 cores * $1/core-hour * 730 hours * 1 replica = $219
	assert.InDelta(t, 219.0, float64(recs[0].EstimatedSavingsUSD), 1.0)
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
			RecMemRequestKiB:     1 * 1024 * 1024,  // 1 GiB
			PodCountAvg:          1,
		},
	}

	ApplySavingsEstimates(recs, cd)

	// memory distribution: 1 GiB * $1/GiB-hr * 730 hrs * 1 pod = $730
	assert.InDelta(t, 730.0, float64(recs[0].EstimatedSavingsUSD), 1.0)
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

	ApplySavingsEstimates(recs, cd)

	// safeDiv returns 0 when denominator is 0
	assert.Equal(t, float32(0), recs[0].EstimatedSavingsUSD)
}
