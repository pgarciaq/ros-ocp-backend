package engine

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const gibKiB = 1024 * 1024

func TestApplyNodeSavings_NilCostData(t *testing.T) {
	recs := []NodeRec{{Node: "worker-1"}}
	ApplyNodeSavings(recs, nil)
	assert.Equal(t, int64(0), recs[0].EstimatedMonthlySavingsCents)
	assert.Contains(t, recs[0].NotificationCodes, NotifNoCostData)
}

func TestApplyNodeSavings_ZeroRates(t *testing.T) {
	recs := []NodeRec{
		{
			CurrentCPUMC:       8000,
			RecommendedCPUMC:   4000,
			CurrentMemKiB:      32 * gibKiB,
			RecommendedMemKiB:  16 * gibKiB,
			NodeCountReduction: 1,
		},
	}
	cd := &costdata.ClusterCostData{ConfiguredRates: map[string]costdata.RatePair{}}
	ApplyNodeSavings(recs, cd)
	assert.Equal(t, int64(0), recs[0].EstimatedMonthlySavingsCents)
	assert.NotContains(t, recs[0].NotificationCodes, NotifNoCostData)
}

func TestApplyNodeSavings_Downsizing(t *testing.T) {
	recs := []NodeRec{
		{
			CurrentCPUMC:       8000,
			RecommendedCPUMC:   4000,
			CurrentMemKiB:      32 * gibKiB,
			RecommendedMemKiB:  16 * gibKiB,
			NodeCountReduction: 1,
		},
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour":  {Infrastructure: 0, Supplementary: 0.01},
			"memory_gb_usage_per_hour": {Infrastructure: 0, Supplementary: 0.02},
			"node_cost_per_month":      {Infrastructure: 1000, Supplementary: 0},
		},
	}
	ApplyNodeSavings(recs, cd)

	require.InDelta(t, 1262.80, money.CentsToUSD(recs[0].EstimatedMonthlySavingsCents), 0.01)
}

func TestApplyNodeSavings_EffectiveMetricFallback(t *testing.T) {
	recs := []NodeRec{
		{
			CurrentCPUMC:       8000,
			RecommendedCPUMC:   4000,
			CurrentMemKiB:      32 * gibKiB,
			RecommendedMemKiB:  16 * gibKiB,
			NodeCountReduction: 1,
		},
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_effective_usage_per_hour":  {Infrastructure: 0, Supplementary: 0.01},
			"memory_gb_effective_usage_per_hour": {Infrastructure: 0, Supplementary: 0.02},
			"node_core_cost_per_month":           {Infrastructure: 1000, Supplementary: 0},
		},
	}
	ApplyNodeSavings(recs, cd)

	require.InDelta(t, 1262.80, money.CentsToUSD(recs[0].EstimatedMonthlySavingsCents), 0.01)
}

func TestApplyNodeSavings_UpsizingNegativeSavings(t *testing.T) {
	recs := []NodeRec{
		{
			CurrentCPUMC:      4000,
			RecommendedCPUMC:  8000,
			CurrentMemKiB:     16 * gibKiB,
			RecommendedMemKiB: 32 * gibKiB,
		},
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour":  {Infrastructure: 0, Supplementary: 0.01},
			"memory_gb_usage_per_hour": {Infrastructure: 0, Supplementary: 0.02},
		},
	}
	ApplyNodeSavings(recs, cd)
	assert.Less(t, recs[0].EstimatedMonthlySavingsCents, int64(0))
}

func TestRecommendedNodeCapacity(t *testing.T) {
	cpu, mem := recommendedNodeCapacity(3500, 8*gibKiB, 0, 0, 0.80)
	assert.Equal(t, int64(5000), cpu)
	assert.Equal(t, int64(10*gibKiB), mem)

	cpuPerf, memPerf := recommendedNodeCapacity(3500, 8*gibKiB, 0, 0, 0.55)
	assert.Greater(t, cpuPerf, cpu)
	assert.Greater(t, memPerf, mem)
}

func TestComputeNodeSavings(t *testing.T) {
	rec := &NodeRec{
		CurrentCPUMC:      10000,
		RecommendedCPUMC:  6000,
		CurrentMemKiB:     40 * gibKiB,
		RecommendedMemKiB: 20 * gibKiB,
	}
	savings := computeNodeSavings(rec, 0.007, 0.009, 0)
	require.InDelta(t, 151.84, savings, 0.01)
}

func TestHasFullSpareNodeHeadroom(t *testing.T) {
	assert.True(t, hasFullSpareNodeHeadroom(16000, 64*gibKiB, 4000, 16*gibKiB, 2.0))
	assert.False(t, hasFullSpareNodeHeadroom(16000, 64*gibKiB, 9000, 32*gibKiB, 2.0))
	assert.False(t, hasFullSpareNodeHeadroom(0, 64*gibKiB, 4000, 16*gibKiB, 2.0))
}
