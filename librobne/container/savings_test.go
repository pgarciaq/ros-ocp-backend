package container

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

func TestReplicaCountForSavings_PrefersDesiredOverPodCount(t *testing.T) {
	tests := []struct {
		name            string
		desiredReplicas int64
		podCountAvg     int64
		want            float64
	}{
		{"desired > 0, uses desired", 5, 3, 5.0},
		{"desired == 0, falls back to pod count", 0, 3, 3.0},
		{"both zero", 0, 0, 0.0},
		{"desired == 1, pod count == 10", 1, 10, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &types.ContainerRec{
				DesiredReplicas: tt.desiredReplicas,
				PodCountAvg:     tt.podCountAvg,
			}
			got := replicaCountForSavings(rec)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestApplySavingsEstimates_ClearsNoCostDataWhenPriced (#881): NotifNoCostData
// was append-only — a row that once missed cost data kept the warning forever,
// even after a run that priced its namespace and computed savings. The priced
// path must CLEAR the code; a genuinely unpriced namespace keeps it.
func TestApplySavingsEstimates_ClearsNoCostDataWhenPriced(t *testing.T) {
	card := &types.RateCard{
		Currency: "USD",
		Namespaces: map[string]types.NamespaceSpend{
			"priced-ns": {CostModelCPUMicroCents: 100_000_000, CPURequestMilliHours: 100},
		},
	}
	recs := []types.ContainerRec{{
		Namespace:           "priced-ns",
		CurrentCPURequestMC: 1000,
		RecCPURequestMC:     500,
		NotificationCodes:   []int16{types.NotifNoCostData},
	}}
	ApplySavingsEstimates(recs, card, 730)
	assert.NotContains(t, recs[0].NotificationCodes, types.NotifNoCostData,
		"a priced namespace clears the stale no-cost-data code (#881)")
	if recs[0].EstimatedSavingsCents != nil {
		assert.Greater(t, *recs[0].EstimatedSavingsCents, int64(0))
	}

	// Inverse: a genuinely unpriced namespace keeps the code.
	missing := []types.ContainerRec{{
		Namespace:         "unpriced-ns",
		NotificationCodes: []int16{types.NotifNoCostData},
	}}
	ApplySavingsEstimates(missing, card, 730)
	assert.Contains(t, missing[0].NotificationCodes, types.NotifNoCostData)
}

// TestApplySavingsEstimates_ClearsNoCostDataTierA (#881): a non-empty Tier A
// card prices every namespace — the stale code must clear on the Tier A path.
func TestApplySavingsEstimates_ClearsNoCostDataTierA(t *testing.T) {
	card := &types.RateCard{Currency: "USD", CPUMicroCentsPerCoreHour: 1_000_000}
	recs := []types.ContainerRec{{
		Namespace:           "any-ns",
		CurrentCPURequestMC: 1000,
		RecCPURequestMC:     500,
		NotificationCodes:   []int16{types.NotifNoCostData},
	}}
	ApplySavingsEstimates(recs, card, 730)
	assert.NotContains(t, recs[0].NotificationCodes, types.NotifNoCostData,
		"a non-empty Tier A card clears the stale no-cost-data code (#881)")
}
