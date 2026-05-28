package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeClusterQuotaRecommendation_Tighten(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   11000,
		HighRiskThresholdBP:   9000,
		MediumRiskThresholdBP: 7000,
	}
	snap := ClusterQuotaSnapshot{
		ClusterQuotaName: "team-quota",
		CPURequestHardMC: 100000,
		CPURequestUsedMC: 30000,
	}
	nsAgg := NamespaceQuotaClusterAggregate{CPURequestRecommendedMC: 40000}
	rec := computeClusterQuotaRecommendation("org1", "cluster1", snap, nsAgg, cfg)

	assert.Equal(t, QuotaRecTypeTighten, rec.RecommendationType)
	assert.Equal(t, int64(44000), rec.Recommended.CPURequestMillicores)
	assert.Equal(t, int64(56000), rec.CapacityFreed.CPUMillicores)
}

func TestComputeClusterQuotaRecommendation_Raise(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   11000,
		HighRiskThresholdBP:   9000,
		MediumRiskThresholdBP: 7000,
	}
	snap := ClusterQuotaSnapshot{
		ClusterQuotaName: "team-quota",
		CPURequestHardMC: 100000,
		CPURequestUsedMC: 95000,
	}
	nsAgg := NamespaceQuotaClusterAggregate{CPURequestRecommendedMC: 90000}
	rec := computeClusterQuotaRecommendation("org1", "cluster1", snap, nsAgg, cfg)

	assert.Equal(t, QuotaRecTypeRaise, rec.RecommendationType)
	assert.Equal(t, QuotaRiskHigh, rec.RiskLevel)
}

func TestClusterQuotaSnapshot_hasHardLimits(t *testing.T) {
	assert.False(t, (ClusterQuotaSnapshot{}).hasHardLimits())
	assert.True(t, (ClusterQuotaSnapshot{MemoryRequestHardBytes: 1}).hasHardLimits())
}

func TestBpToPercentInt(t *testing.T) {
	bp := 8550
	assert.Equal(t, 85, *bpToPercentInt(&bp))
	assert.Nil(t, bpToPercentInt(nil))
}

func TestComputeClusterQuotaRecommendation_UsedExceedsHard_Raise(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   11000,
		HighRiskThresholdBP:   9000,
		MediumRiskThresholdBP: 7000,
	}
	snap := ClusterQuotaSnapshot{
		ClusterQuotaName: "team-over",
		CPURequestHardMC: 100000,
		CPURequestUsedMC: 110000, // used > hard (quota lowered after allocation)
	}
	rec := computeClusterQuotaRecommendation("org1", "cluster1", snap, NamespaceQuotaClusterAggregate{}, cfg)

	assert.Equal(t, QuotaRecTypeRaise, rec.RecommendationType)
	assert.Equal(t, QuotaRiskHigh, rec.RiskLevel)
	require.NotNil(t, rec.UtilizationCPURequestPercent)
	assert.GreaterOrEqual(t, *rec.UtilizationCPURequestPercent, 100)
}

func TestComputeClusterQuotaRecommendation_ZeroUsedZeroAgg_OptimalNotTighten(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   11000,
		HighRiskThresholdBP:   9000,
		MediumRiskThresholdBP: 7000,
	}
	snap := ClusterQuotaSnapshot{
		ClusterQuotaName: "team-empty",
		CPURequestHardMC: 100000,
		CPURequestUsedMC: 0,
	}
	rec := computeClusterQuotaRecommendation("org1", "cluster1", snap, NamespaceQuotaClusterAggregate{}, cfg)

	assert.Equal(t, QuotaRecTypeOptimal, rec.RecommendationType)
	assert.Equal(t, int64(0), rec.Recommended.CPURequestMillicores)
}

func TestRecommendClusterQuotas_SkipsZeroHardLimit(t *testing.T) {
	cfg := QuotaRecConfig{HeadroomBasisPoints: 11000, HighRiskThresholdBP: 9000, MediumRiskThresholdBP: 7000}
	snap := ClusterQuotaSnapshot{ClusterQuotaName: "no-hard"}
	assert.False(t, snap.hasHardLimits())
	rec := computeClusterQuotaRecommendation("org1", "cluster1", snap, NamespaceQuotaClusterAggregate{}, cfg)
	assert.Equal(t, QuotaRecTypeNone, rec.RecommendationType)
}
