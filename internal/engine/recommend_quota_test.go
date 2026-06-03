package engine

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeQuotaRecommendation_Tighten(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   12000,
		HighRiskThresholdBP:   8000,
		MediumRiskThresholdBP: 6000,
	}
	snap := NamespaceQuotaSnapshot{
		Namespace:        "app",
		CPURequestHardMC: 100000,
		CPURequestUsedMC: 25000,
	}
	agg := ContainerQuotaAggregate{CPURequestSumMC: 30000}
	rec := computeQuotaRecommendation("org1", "cluster1", snap, agg, cfg)

	assert.Equal(t, QuotaRecTypeTighten, rec.RecommendationType)
	assert.Equal(t, int64(36000), rec.Recommended.CPURequestMillicores)
	assert.Equal(t, int64(64000), rec.CapacityFreed.CPUMillicores)
	assert.Equal(t, QuotaRiskLow, rec.RiskLevel)
}

func TestComputeQuotaRecommendation_Raise(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   12000,
		HighRiskThresholdBP:   8000,
		MediumRiskThresholdBP: 6000,
	}
	snap := NamespaceQuotaSnapshot{
		Namespace:        "app",
		CPURequestHardMC: 100000,
		CPURequestUsedMC: 85000,
	}
	agg := ContainerQuotaAggregate{CPURequestSumMC: 10000}
	rec := computeQuotaRecommendation("org1", "cluster1", snap, agg, cfg)

	assert.Equal(t, QuotaRecTypeRaise, rec.RecommendationType)
	assert.Equal(t, QuotaRiskHigh, rec.RiskLevel)
}

func TestComputeQuotaRecommendation_SignalC_UsesContainerRecSum(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   12000,
		HighRiskThresholdBP:   8000,
		MediumRiskThresholdBP: 6000,
	}
	snap := NamespaceQuotaSnapshot{
		Namespace:        "app",
		CPURequestHardMC: 100000,
		CPURequestUsedMC: 10000,
	}
	agg := ContainerQuotaAggregate{CPURequestSumMC: 90000}
	rec := computeQuotaRecommendation("org1", "cluster1", snap, agg, cfg)

	assert.Equal(t, QuotaRecTypeRaise, rec.RecommendationType)
	assert.NotNil(t, rec.Utilization.CPURequestBP)
	assert.Equal(t, 9000, *rec.Utilization.CPURequestBP)
}

func TestApplyHeadroom(t *testing.T) {
	assert.Equal(t, int64(1200), applyHeadroom(1000, 12000))
	assert.Equal(t, int64(0), applyHeadroom(0, 12000))
}

func TestUtilizationBP(t *testing.T) {
	bp := utilizationBP(25, 100)
	assert.NotNil(t, bp)
	assert.Equal(t, 2500, *bp)
	assert.Nil(t, utilizationBP(10, 0))
}

func TestQuotaRecConfigFromApp(t *testing.T) {
	appCfg := &config.Config{
		QuotaHeadroomPercent:            10,
		QuotaHighRiskThresholdPercent:   90,
		QuotaMediumRiskThresholdPercent: 70,
	}
	cfg := QuotaRecConfigFromApp(appCfg)
	assert.Equal(t, 11000, cfg.HeadroomBasisPoints)
	assert.Equal(t, 9000, cfg.HighRiskThresholdBP)
	assert.Equal(t, 7000, cfg.MediumRiskThresholdBP)
}

func TestQuotaRecConfigFromApp_TenPercentHeadroom(t *testing.T) {
	cfg := QuotaRecConfigFromApp(&config.Config{QuotaHeadroomPercent: 10})
	assert.Equal(t, 11000, cfg.HeadroomBasisPoints)
}

func TestNamespaceQuotaSnapshot_hasHardLimits(t *testing.T) {
	assert.False(t, (NamespaceQuotaSnapshot{}).hasHardLimits())
	assert.True(t, (NamespaceQuotaSnapshot{CPURequestHardMC: 1}).hasHardLimits())
	assert.True(t, (NamespaceQuotaSnapshot{MemoryLimitHardBytes: 1024}).hasHardLimits())
}

func TestComputeQuotaRecommendation_Optimal(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   20000,
		HighRiskThresholdBP:   8000,
		MediumRiskThresholdBP: 6000,
	}
	snap := NamespaceQuotaSnapshot{
		Namespace:        "app",
		CPURequestHardMC: 100000,
		CPURequestUsedMC: 10000,
	}
	agg := ContainerQuotaAggregate{CPURequestSumMC: 50000}
	rec := computeQuotaRecommendation("org1", "cluster1", snap, agg, cfg)

	assert.Equal(t, QuotaRecTypeOptimal, rec.RecommendationType)
	assert.Equal(t, int64(100000), rec.Recommended.CPURequestMillicores)
	assert.Equal(t, QuotaRiskLow, rec.RiskLevel)
	assert.Zero(t, rec.CapacityFreed.CPUMillicores)
}

func TestComputeQuotaRecommendation_ObjectCountHighRisk(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   11000,
		HighRiskThresholdBP:   9000,
		MediumRiskThresholdBP: 7000,
	}
	snap := NamespaceQuotaSnapshot{
		Namespace:        "app",
		ObjectCountHard:  100,
		ObjectCountUsed:  95,
		CPURequestHardMC: 1000000,
		CPURequestUsedMC: 1000,
	}
	rec := computeQuotaRecommendation("org1", "cluster1", snap, ContainerQuotaAggregate{}, cfg)

	assert.Equal(t, QuotaRiskHigh, rec.RiskLevel)
	assert.Equal(t, QuotaRecTypeRaise, rec.RecommendationType)
	require.NotNil(t, rec.Utilization.ObjectCountBP)
	assert.Equal(t, 9500, *rec.Utilization.ObjectCountBP)
}

func TestQuotaNotificationCodes_ObjectCountBlocking(t *testing.T) {
	snap := NamespaceQuotaSnapshot{
		ObjectCountHard: 50,
		ObjectCountUsed: 50,
	}
	rec := QuotaRec{RiskLevel: QuotaRiskLow, RecommendationType: QuotaRecTypeOptimal}
	codes := QuotaNotificationCodes(snap, rec)
	assert.Contains(t, codes, NotifQuotaBlocking)
}

func TestComputeQuotaRecommendation_MediumRisk(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   10000,
		HighRiskThresholdBP:   8000,
		MediumRiskThresholdBP: 6000,
	}
	snap := NamespaceQuotaSnapshot{
		Namespace:        "app",
		CPURequestHardMC: 100000,
		CPURequestUsedMC: 65000,
	}
	agg := ContainerQuotaAggregate{}
	rec := computeQuotaRecommendation("org1", "cluster1", snap, agg, cfg)

	assert.Equal(t, QuotaRiskMedium, rec.RiskLevel)
	require.NotNil(t, rec.Utilization.CPURequestBP)
	assert.Equal(t, 6500, *rec.Utilization.CPURequestBP)
}

func TestComputeQuotaRecommendation_LowRisk(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   10000,
		HighRiskThresholdBP:   8000,
		MediumRiskThresholdBP: 6000,
	}
	snap := NamespaceQuotaSnapshot{
		Namespace:        "app",
		CPURequestHardMC: 100000,
		CPURequestUsedMC: 30000,
	}
	rec := computeQuotaRecommendation("org1", "cluster1", snap, ContainerQuotaAggregate{}, cfg)

	assert.Equal(t, QuotaRiskLow, rec.RiskLevel)
}

func TestComputeQuotaRecommendation_ZeroContainerAgg_OptimalNoTighten(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   12000,
		HighRiskThresholdBP:   8000,
		MediumRiskThresholdBP: 6000,
	}
	snap := NamespaceQuotaSnapshot{
		Namespace:        "app",
		CPURequestHardMC: 100000,
		CPURequestUsedMC: 10000,
	}
	rec := computeQuotaRecommendation("org1", "cluster1", snap, ContainerQuotaAggregate{}, cfg)

	assert.Equal(t, QuotaRecTypeOptimal, rec.RecommendationType)
	assert.Zero(t, rec.Recommended.CPURequestMillicores)
	assert.Equal(t, QuotaRiskLow, rec.RiskLevel)
}

func TestComputeQuotaRecommendation_ZeroUsed_UsesAggregateForUtilization(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   10000,
		HighRiskThresholdBP:   8000,
		MediumRiskThresholdBP: 6000,
	}
	snap := NamespaceQuotaSnapshot{
		Namespace:        "app",
		CPURequestHardMC: 100000,
	}
	agg := ContainerQuotaAggregate{CPURequestSumMC: 85000}
	rec := computeQuotaRecommendation("org1", "cluster1", snap, agg, cfg)

	assert.Equal(t, QuotaRecTypeRaise, rec.RecommendationType)
	require.NotNil(t, rec.Utilization.CPURequestBP)
	assert.Equal(t, 8500, *rec.Utilization.CPURequestBP)
}

func TestClassifyQuotaRisk_NoneWhenNoUtilization(t *testing.T) {
	cfg := QuotaRecConfig{HighRiskThresholdBP: 8000, MediumRiskThresholdBP: 6000}
	assert.Equal(t, QuotaRiskNone, classifyQuotaRisk(QuotaUtilizationBP{}, cfg))
}

func TestMaxUtilizationBP_IgnoresNilPointers(t *testing.T) {
	cpu := 4500
	assert.Equal(t, 4500, maxUtilizationBP(QuotaUtilizationBP{CPURequestBP: &cpu}))
}

func TestApplyQuotaSavings_NilCostData(t *testing.T) {
	recs := []QuotaRec{{
		RecommendationType: QuotaRecTypeTighten,
		CapacityFreed:      QuotaCapacityFreed{CPUMillicores: 1000},
	}}
	ApplyQuotaSavings(recs, nil)
	assert.Zero(t, recs[0].EstimatedSavingsCents)
}

func TestApplyQuotaSavings_TightenOnly(t *testing.T) {
	recs := []QuotaRec{
		{
			RecommendationType: QuotaRecTypeTighten,
			CapacityFreed:      QuotaCapacityFreed{CPUMillicores: 2000, MemoryBytes: 0},
		},
		{
			RecommendationType: QuotaRecTypeRaise,
			CapacityFreed:      QuotaCapacityFreed{CPUMillicores: 5000},
		},
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour":    {Supplementary: 1.0},
			"memory_gb_usage_per_hour":   {Supplementary: 2.0},
		},
	}
	ApplyQuotaSavings(recs, cd)

	// 2 cores freed * $1/core-hour * 730 h/month = $1460
	assert.Equal(t, int64(146000), recs[0].EstimatedSavingsCents)
	assert.Zero(t, recs[1].EstimatedSavingsCents)
}

func TestComputeQuotaRecommendation_StorageTighten(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   12000,
		HighRiskThresholdBP:   8000,
		MediumRiskThresholdBP: 6000,
	}
	snap := NamespaceQuotaSnapshot{
		Namespace:               "app",
		StorageRequestHardBytes: 10 * gib,
		StorageRequestUsedBytes: 2 * gib,
	}
	rec := computeQuotaRecommendation("org1", "cluster1", snap, ContainerQuotaAggregate{}, cfg)

	assert.Equal(t, QuotaRecTypeTighten, rec.RecommendationType)
	assert.Equal(t, int64(2576980377), rec.Recommended.StorageRequestBytes)
	assert.Equal(t, int64(8160437863), rec.CapacityFreed.StorageBytes)
}

func TestComputeQuotaRecommendation_PodsRaise(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   10000,
		HighRiskThresholdBP:   8000,
		MediumRiskThresholdBP: 6000,
	}
	snap := NamespaceQuotaSnapshot{
		Namespace: "app",
		PodsHard:  50,
		PodsUsed:  45,
	}
	rec := computeQuotaRecommendation("org1", "cluster1", snap, ContainerQuotaAggregate{}, cfg)

	assert.Equal(t, QuotaRecTypeRaise, rec.RecommendationType)
	assert.Equal(t, QuotaRiskHigh, rec.RiskLevel)
	require.NotNil(t, rec.Utilization.PodsBP)
	assert.Equal(t, 9000, *rec.Utilization.PodsBP)
}

func TestApplyQuotaSavings_StorageFreed(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	recs := []QuotaRec{{
		RecommendationType: QuotaRecTypeTighten,
		CapacityFreed:      QuotaCapacityFreed{StorageBytes: gib},
	}}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"storage_gb_request_per_month": {Supplementary: 0.10},
		},
	}
	ApplyQuotaSavings(recs, cd)
	assert.Equal(t, int64(10), recs[0].EstimatedSavingsCents)
}

func TestApplyQuotaSavings_MemoryFreed(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	recs := []QuotaRec{{
		RecommendationType: QuotaRecTypeTighten,
		CapacityFreed:      QuotaCapacityFreed{MemoryBytes: 2 * gib},
	}}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"memory_gb_usage_per_hour": {Supplementary: 1.0},
		},
	}
	ApplyQuotaSavings(recs, cd)

	// 2 GiB * $1/GiB-hour * 730 h = $1460
	assert.Equal(t, int64(146000), recs[0].EstimatedSavingsCents)
}
