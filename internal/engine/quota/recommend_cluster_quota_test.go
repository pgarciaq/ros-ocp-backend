package quota

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/stretchr/testify/assert"
)

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
	ApplyClusterQuotaSavings(recs, cd, 730)
	assert.Equal(t, int64(5000), recs[0].EstimatedSavingsCents)
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
	ApplyClusterQuotaSavings(recs, cd, 730)
	assert.Equal(t, int64(0), recs[0].EstimatedSavingsCents)
	assert.Equal(t, int64(20), recs[0].CapacityFreed.PodsFreed)
}
