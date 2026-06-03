package engine

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyPVCSavings_NilCostData(t *testing.T) {
	recs := []PVCRec{{Namespace: "app", PVC: "data"}}
	ApplyPVCSavings(recs, nil)
	assert.Equal(t, int64(0), recs[0].EstimatedMonthlySavingsCents)
	assert.Contains(t, recs[0].NotificationCodes, NotifNoCostData)
}

func TestApplyPVCSavings_ZeroRates(t *testing.T) {
	recommended := int64(10 * 1024 * 1024 * 1024)
	recs := []PVCRec{
		{
			RequestBytes:     100 * 1024 * 1024 * 1024,
			RecommendedBytes: &recommended,
		},
	}
	cd := &costdata.ClusterCostData{ConfiguredRates: map[string]costdata.RatePair{}}
	ApplyPVCSavings(recs, cd)
	assert.Equal(t, int64(0), recs[0].EstimatedMonthlySavingsCents)
	assert.NotContains(t, recs[0].NotificationCodes, NotifNoCostData)
}

func TestApplyPVCSavings_Downsizing(t *testing.T) {
	recommended := int64(10 * 1024 * 1024 * 1024)
	recs := []PVCRec{
		{
			RequestBytes:     100 * 1024 * 1024 * 1024,
			RecommendedBytes: &recommended,
		},
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"storage_gb_request_per_month": {Infrastructure: 0, Supplementary: 0.10},
		},
	}
	ApplyPVCSavings(recs, cd)
	// 90 GiB * $0.10 = $9.00
	require.InDelta(t, 9.0, money.CentsToUSD(recs[0].EstimatedMonthlySavingsCents), 0.01)
}

func TestApplyPVCSavings_FallbackToUsageRate(t *testing.T) {
	recommended := int64(5 * 1024 * 1024 * 1024)
	recs := []PVCRec{
		{
			RequestBytes:     20 * 1024 * 1024 * 1024,
			RecommendedBytes: &recommended,
		},
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"storage_gb_usage_per_month": {Infrastructure: 0, Supplementary: 0.05},
		},
	}
	ApplyPVCSavings(recs, cd)
	// 15 GiB * $0.05 = $0.75
	require.InDelta(t, 0.75, money.CentsToUSD(recs[0].EstimatedMonthlySavingsCents), 0.01)
}

func TestApplyPVCSavings_UpsizingNegativeSavings(t *testing.T) {
	recommended := int64(200 * 1024 * 1024 * 1024)
	recs := []PVCRec{
		{
			RequestBytes:     100 * 1024 * 1024 * 1024,
			RecommendedBytes: &recommended,
		},
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"storage_gb_request_per_month": {Infrastructure: 0, Supplementary: 0.10},
		},
	}
	ApplyPVCSavings(recs, cd)
	assert.Less(t, recs[0].EstimatedMonthlySavingsCents, int64(0))
}

func TestApplyPVCSavings_Orphaned(t *testing.T) {
	recs := []PVCRec{
		{
			RecommendationType: PVCRecTypeOrphaned,
			RequestBytes:       50 * 1024 * 1024 * 1024,
		},
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"storage_gb_request_per_month": {Infrastructure: 0, Supplementary: 0.10},
		},
	}
	ApplyPVCSavings(recs, cd)
	// 50 GiB * $0.10 = $5.00 (full monthly cost recoverable by deletion)
	require.InDelta(t, 5.0, money.CentsToUSD(recs[0].EstimatedMonthlySavingsCents), 0.01)
}

func TestApplyPVCSavings_NoRecommendation(t *testing.T) {
	recs := []PVCRec{
		{
			RequestBytes: 100 * 1024 * 1024 * 1024,
		},
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"storage_gb_request_per_month": {Infrastructure: 0, Supplementary: 0.10},
		},
	}
	ApplyPVCSavings(recs, cd)
	assert.Equal(t, int64(0), recs[0].EstimatedMonthlySavingsCents)
}

func TestStorageRequestPerMonth(t *testing.T) {
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"storage_gb_request_per_month": {Infrastructure: 0.01, Supplementary: 0.02},
			"storage_gb_usage_per_month":   {Infrastructure: 0.50, Supplementary: 0},
		},
	}
	assert.InDelta(t, 0.03, StorageRequestPerMonth(cd), 0.0001)

	cd.ConfiguredRates["storage_gb_request_per_month"] = costdata.RatePair{}
	assert.InDelta(t, 0.50, StorageRequestPerMonth(cd), 0.0001)
}
