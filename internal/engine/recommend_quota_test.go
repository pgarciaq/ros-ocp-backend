package engine

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/stretchr/testify/assert"
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
		QuotaHeadroomPercent:            20,
		QuotaHighRiskThresholdPercent:   80,
		QuotaMediumRiskThresholdPercent: 60,
	}
	cfg := QuotaRecConfigFromApp(appCfg)
	assert.Equal(t, 12000, cfg.HeadroomBasisPoints)
	assert.Equal(t, 8000, cfg.HighRiskThresholdBP)
	assert.Equal(t, 6000, cfg.MediumRiskThresholdBP)
}
