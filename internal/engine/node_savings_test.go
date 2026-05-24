package engine

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyNodeSavings_NilCostData(t *testing.T) {
	t.Parallel()
	recs := []NodeRec{{Node: "worker-1"}}
	ApplyNodeSavings(recs, nil)
	assert.Equal(t, float32(0), recs[0].EstimatedMonthlySavingsUSD)
	assert.Contains(t, recs[0].NotificationCodes, NotifNoCostData)
}

func TestApplyNodeSavings_ZeroRates(t *testing.T) {
	t.Parallel()
	recs := []NodeRec{
		{
			CurrentCPUCores:      8,
			RecommendedCPUCores:  4,
			CurrentMemoryGiB:     32,
			RecommendedMemoryGiB: 16,
			NodeCountReduction:   1,
		},
	}
	cd := &costdata.ClusterCostData{ConfiguredRates: map[string]costdata.RatePair{}}
	ApplyNodeSavings(recs, cd)
	assert.Equal(t, float32(0), recs[0].EstimatedMonthlySavingsUSD)
	assert.NotContains(t, recs[0].NotificationCodes, NotifNoCostData)
}

func TestApplyNodeSavings_Downsizing(t *testing.T) {
	t.Parallel()
	recs := []NodeRec{
		{
			CurrentCPUCores:      8,
			RecommendedCPUCores:  4,
			CurrentMemoryGiB:     32,
			RecommendedMemoryGiB: 16,
			NodeCountReduction:   1,
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

	// CPU: 4 cores * $0.01/hr * 730 = $29.20
	// Mem: 16 GiB * $0.02/hr * 730 = $233.60
	// Node: 1 * $1000 = $1000
	// Total = $1262.80
	require.InDelta(t, 1262.80, float64(recs[0].EstimatedMonthlySavingsUSD), 0.01)
}

func TestApplyNodeSavings_UpsizingNegativeSavings(t *testing.T) {
	t.Parallel()
	recs := []NodeRec{
		{
			CurrentCPUCores:      4,
			RecommendedCPUCores:  8,
			CurrentMemoryGiB:     16,
			RecommendedMemoryGiB: 32,
		},
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour":  {Infrastructure: 0, Supplementary: 0.01},
			"memory_gb_usage_per_hour": {Infrastructure: 0, Supplementary: 0.02},
		},
	}
	ApplyNodeSavings(recs, cd)
	assert.Less(t, recs[0].EstimatedMonthlySavingsUSD, float32(0))
}

func TestRecommendedNodeCapacity(t *testing.T) {
	t.Parallel()
	cpu, mem := recommendedNodeCapacity(3500, 8*1024*1024, 0, 0, 0.80)
	assert.Equal(t, float64(5), cpu)
	assert.Equal(t, float64(10), mem)

	cpuPerf, memPerf := recommendedNodeCapacity(3500, 8*1024*1024, 0, 0, 0.55)
	assert.Greater(t, cpuPerf, cpu)
	assert.Greater(t, memPerf, mem)
}

func TestComputeNodeSavings(t *testing.T) {
	t.Parallel()
	rec := &NodeRec{
		CurrentCPUCores:      10,
		RecommendedCPUCores:  6,
		CurrentMemoryGiB:     40,
		RecommendedMemoryGiB: 20,
	}
	savings := computeNodeSavings(rec, 0.007, 0.009, 0)
	// CPU: 4 * 0.007 * 730 = 20.44
	// Mem: 20 * 0.009 * 730 = 131.40
	require.InDelta(t, 151.84, savings, 0.01)
}
