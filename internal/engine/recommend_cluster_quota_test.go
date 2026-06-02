package engine

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
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

func TestComputeClusterQuotaRecommendation_StorageTighten_CapacityFreed(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   11000,
		HighRiskThresholdBP:   9000,
		MediumRiskThresholdBP: 7000,
	}
	const gib = 1024 * 1024 * 1024
	snap := ClusterQuotaSnapshot{
		ClusterQuotaName:        "team-storage",
		StorageRequestHardBytes: 10 * gib,
		StorageRequestUsedBytes: 2 * gib,
	}
	rec := computeClusterQuotaRecommendation("org1", "cluster1", snap, NamespaceQuotaClusterAggregate{}, cfg)

	assert.Equal(t, QuotaRecTypeTighten, rec.RecommendationType)
	assert.Equal(t, int64(2*gib*11000/10000), rec.StorageRecommendedBytes)
	assert.Equal(t, int64(10*gib-rec.StorageRecommendedBytes), rec.CapacityFreed.StorageBytes)
	assert.Equal(t, int64(0), rec.CapacityFreed.MemoryBytes)
}

func TestApplyClusterQuotaSavings_Storage(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	recs := []ClusterQuotaRec{{
		RecommendationType: QuotaRecTypeTighten,
		CapacityFreed: QuotaCapacityFreed{
			StorageBytes: 5 * gib,
		},
	}}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"storage_gb_request_per_month": {Infrastructure: 0, Supplementary: 10},
		},
	}
	ApplyClusterQuotaSavings(recs, cd)
	assert.Equal(t, 50, recs[0].SavingsDollarsMonthly)
}

func TestApplyClusterQuotaSavings_PodsNoMonetarySavings(t *testing.T) {
	recs := []ClusterQuotaRec{{
		RecommendationType: QuotaRecTypeTighten,
		CapacityFreed:      QuotaCapacityFreed{PodsFreed: 20},
	}}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour": {Infrastructure: 1, Supplementary: 0},
		},
	}
	ApplyClusterQuotaSavings(recs, cd)
	assert.Equal(t, 0, recs[0].SavingsDollarsMonthly)
	assert.Equal(t, int64(20), recs[0].CapacityFreed.PodsFreed)
}

func TestComputeClusterQuotaRecommendation_ObjectCountHighRisk(t *testing.T) {
	cfg := QuotaRecConfig{
		HeadroomBasisPoints:   11000,
		HighRiskThresholdBP:   9000,
		MediumRiskThresholdBP: 7000,
	}
	snap := ClusterQuotaSnapshot{
		ClusterQuotaName: "team-objects",
		ObjectCountHard:  100,
		ObjectCountUsed:  95,
		CPURequestHardMC: 1000000,
		CPURequestUsedMC: 1000,
	}
	rec := computeClusterQuotaRecommendation("org1", "cluster1", snap, NamespaceQuotaClusterAggregate{}, cfg)

	assert.Equal(t, QuotaRiskHigh, rec.RiskLevel)
	assert.Equal(t, QuotaRecTypeRaise, rec.RecommendationType)
}

func TestClusterQuotaNotificationCodes_ObjectCountBlocking(t *testing.T) {
	rec := ClusterQuotaRec{
		Snapshot: ClusterQuotaSnapshot{
			ObjectCountHard: 50,
			ObjectCountUsed: 50,
		},
		RecommendationType: QuotaRecTypeOptimal,
		RiskLevel:          QuotaRiskLow,
	}
	codes := ClusterQuotaNotificationCodes(rec)
	assert.Contains(t, codes, NotifQuotaBlocking)
}

func TestRecommendClusterQuotas_SkipsZeroHardLimit(t *testing.T) {
	cfg := QuotaRecConfig{HeadroomBasisPoints: 11000, HighRiskThresholdBP: 9000, MediumRiskThresholdBP: 7000}
	snap := ClusterQuotaSnapshot{ClusterQuotaName: "no-hard"}
	assert.False(t, snap.hasHardLimits())
	rec := computeClusterQuotaRecommendation("org1", "cluster1", snap, NamespaceQuotaClusterAggregate{}, cfg)
	assert.Equal(t, QuotaRecTypeNone, rec.RecommendationType)
}
