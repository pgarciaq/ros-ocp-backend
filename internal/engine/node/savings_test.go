package node

import (
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const gibKiB = 1024 * 1024

func TestApplyNodeSavings_NilCostData(t *testing.T) {
	recs := []Rec{{Node: "worker-1"}}
	ApplyNodeSavings(recs, nil, 730)
	assert.Equal(t, int64(0), recs[0].EstimatedMonthlySavingsCents)
	assert.Contains(t, recs[0].NotificationCodes, core.NotifNoCostData)
}

func TestApplyNodeSavings_ZeroRates(t *testing.T) {
	recs := []Rec{
		{
			CurrentCPUMC:       8000,
			RecommendedCPUMC:   4000,
			CurrentMemKiB:      32 * gibKiB,
			RecommendedMemKiB:  16 * gibKiB,
			NodeCountReduction: 1,
		},
	}
	cd := &costdata.ClusterCostData{ConfiguredRates: map[string]costdata.RatePair{}}
	ApplyNodeSavings(recs, cd, 730)
	assert.Equal(t, int64(0), recs[0].EstimatedMonthlySavingsCents)
	assert.NotContains(t, recs[0].NotificationCodes, core.NotifNoCostData)
}

func TestApplyNodeSavings_Downsizing(t *testing.T) {
	recs := []Rec{
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
	ApplyNodeSavings(recs, cd, 730)

	require.InDelta(t, 1262.80, money.CentsToUSD(recs[0].EstimatedMonthlySavingsCents), 0.01)
}

func TestApplyNodeSavings_EffectiveMetricFallback(t *testing.T) {
	recs := []Rec{
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
	ApplyNodeSavings(recs, cd, 730)

	require.InDelta(t, 1262.80, money.CentsToUSD(recs[0].EstimatedMonthlySavingsCents), 0.01)
}

func TestApplyNodeSavings_UpsizingNegativeSavings(t *testing.T) {
	recs := []Rec{
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
	ApplyNodeSavings(recs, cd, 730)
	assert.Less(t, recs[0].EstimatedMonthlySavingsCents, int64(0))
}

func TestHasFullSpareNodeHeadroom(t *testing.T) {
	rec := &Rec{
		CurrentCPUMC:      10000,
		RecommendedCPUMC:  6000,
		CurrentMemKiB:     40 * gibKiB,
		RecommendedMemKiB: 20 * gibKiB,
	}
	savings := computeNodeSavings(rec, 0.007, 0.009, 0, 730)
	require.InDelta(t, 151.84, savings, 0.01)
}

func TestRecommendNodes_EngineSavingsDiffer(t *testing.T) {
	cfg := RecConfigFromThresholds(DefaultThresholdSettings())
	allocCPU := int64(16000)
	allocMem := int64(65536)
	ptr := func(v int64) *int64 { return &v }
	makeRow := func(day int, cpuP50, cpuP95, memP50, memP95, cpuReqs, memReqs int64) DigestRow {
		return DigestRow{
			BucketDate:        time.Date(2026, 5, day, 0, 0, 0, 0, time.UTC),
			Node:              "node-savings",
			CPUUsageP50MC:     cpuP50,
			CPUUsageP95MC:     cpuP95,
			MemUsageP50KiB:    memP50,
			MemUsageP95KiB:    memP95,
			MaxCPUAllocMC:     ptr(allocCPU),
			MaxMemAllocKiB:    ptr(allocMem),
			MaxCPURequestsMC:  cpuReqs,
			MaxMemRequestsKiB: memReqs,
			MaxPodCount:       10,
			SampleCount:       24,
		}
	}
	digests := []DigestRow{
		makeRow(1, 500, 1000, 2000, 4000, 8000, 32000),
		makeRow(2, 600, 1200, 2500, 4500, 8000, 32000),
		makeRow(3, 550, 1100, 2200, 4200, 8000, 32000),
	}
	terms := []core.TermConfig{{Name: "medium", WindowDays: 30, MinDataDays: 3}}
	results := RecommendNodes(digests, cfg, DefaultThresholdSettings(), terms)
	byEngine := map[string]Rec{}
	for _, r := range results {
		byEngine[r.Node+"/"+r.Engine] = r
	}
	costRec := byEngine["node-savings/cost"]
	perfRec := byEngine["node-savings/performance"]
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour":  {Infrastructure: 0, Supplementary: 0.01},
			"memory_gb_usage_per_hour": {Infrastructure: 0, Supplementary: 0.02},
			"node_cost_per_month":      {Infrastructure: 1000, Supplementary: 0},
		},
	}
	recs := []Rec{costRec, perfRec}
	ApplyNodeSavings(recs, cd, 730)
	assert.Greater(t, recs[0].EstimatedMonthlySavingsCents, recs[1].EstimatedMonthlySavingsCents,
		"cost engine should show higher savings than performance for underutilized node")
}
