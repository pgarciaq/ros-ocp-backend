package quota

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/stretchr/testify/assert"
)

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
	assert.Equal(t, money.DefaultCurrency, cfg.Currency)
}

func TestQuotaRecConfigFromApp_TenPercentHeadroom(t *testing.T) {
	cfg := QuotaRecConfigFromApp(&config.Config{QuotaHeadroomPercent: 10})
	assert.Equal(t, 11000, cfg.HeadroomBasisPoints)
	assert.Equal(t, money.DefaultCurrency, cfg.Currency)
}

func TestApplyQuotaSavings_NilCostData(t *testing.T) {
	recs := []QuotaRec{{
		RecommendationType: QuotaRecTypeTighten,
		CapacityFreed:      QuotaCapacityFreed{CPUMillicores: 1000},
	}}
	ApplyQuotaSavings(recs, nil, 730)
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
			"cpu_core_usage_per_hour":  {Supplementary: 1.0},
			"memory_gb_usage_per_hour": {Supplementary: 2.0},
		},
	}
	ApplyQuotaSavings(recs, cd, 730)

	// 2 cores freed * $1/core-hour * 730 h/month = $1460
	assert.Equal(t, int64(146000), recs[0].EstimatedSavingsCents)
	assert.Zero(t, recs[1].EstimatedSavingsCents)
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
	ApplyQuotaSavings(recs, cd, 730)
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
	ApplyQuotaSavings(recs, cd, 730)

	// 2 GiB * $1/GiB-hour * 730 h = $1460
	assert.Equal(t, int64(146000), recs[0].EstimatedSavingsCents)
}
