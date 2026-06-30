package fleetsummary

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

func sampleSummary(total int) CachedSummary {
	return CachedSummary{
		TotalContainers:     total,
		Currency:            "USD",
		TotalMonthlySavings: money.FormatUSDToAmount(0, "USD"),
	}
}

func TestCache_GetPutInvalidate(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_TTL", "300")

	orgID := "1234567"
	summary := sampleSummary(10)

	Put(orgID, false, nil, summary)
	got, ok := Get(orgID, false, nil)
	assert.True(t, ok)
	assert.Equal(t, 10, got.TotalContainers)

	InvalidateOrg(orgID)
	_, ok = Get(orgID, false, nil)
	assert.False(t, ok)
}

func TestCache_MetricsHitMissRemovalInvalidation(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_CAPACITY", "2")
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_TTL", "3600")
	ResetForTest()

	orgID := "org-metrics"
	beforeHits := counterValue(t, "rosocp_fleet_summary_cache_hits_total")
	beforeMisses := counterValue(t, "rosocp_fleet_summary_cache_misses_total")
	beforeRemovals := counterValue(t, "rosocp_fleet_summary_cache_removals_total")
	beforeInvalidations := counterValue(t, "rosocp_fleet_summary_cache_invalidations_total")

	_, ok := Get(orgID, false, nil)
	assert.False(t, ok)
	assert.Equal(t, beforeMisses+1, counterValue(t, "rosocp_fleet_summary_cache_misses_total"))

	Put(orgID, false, nil, sampleSummary(1))
	Put(orgID+"b", false, nil, sampleSummary(2))
	Put(orgID+"c", false, nil, sampleSummary(3))

	assert.GreaterOrEqual(t, counterValue(t, "rosocp_fleet_summary_cache_removals_total"), beforeRemovals+1)
	assert.Equal(t, float64(2), gaugeValue(t, "rosocp_fleet_summary_cache_size"))

	_, ok = Get(orgID+"c", false, nil)
	assert.True(t, ok)
	assert.Equal(t, beforeHits+1, counterValue(t, "rosocp_fleet_summary_cache_hits_total"))

	InvalidateOrg(orgID)
	assert.Equal(t, beforeInvalidations+1, counterValue(t, "rosocp_fleet_summary_cache_invalidations_total"))
}

func TestSavingsCache_GetPutInvalidateMetrics(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_TTL", "3600")
	ResetForTest()

	orgID := "org-savings-cache"
	engine := "cost"
	term := "medium"
	summary := CachedSavingsSummary{
		Currency:                "USD",
		EstimatedMonthlySavings: money.FormatUSDToAmount(0, "USD"),
		ByCluster:               []CachedClusterSavings{},
		ByPlugin:                CachedSavingsByPlugin{Container: money.FormatUSDToAmount(0, "USD")},
		GPUSavingsNote:          "note",
	}

	beforeHits := counterValue(t, "rosocp_savings_summary_cache_hits_total")
	beforeMisses := counterValue(t, "rosocp_savings_summary_cache_misses_total")

	_, ok := GetSavings(orgID, false, nil, engine, term)
	assert.False(t, ok)
	assert.Equal(t, beforeMisses+1, counterValue(t, "rosocp_savings_summary_cache_misses_total"))

	PutSavings(orgID, false, nil, engine, term, summary)
	got, ok := GetSavings(orgID, false, nil, engine, term)
	assert.True(t, ok)
	assert.Equal(t, "USD", got.Currency)
	assert.Equal(t, beforeHits+1, counterValue(t, "rosocp_savings_summary_cache_hits_total"))

	InvalidateOrg(orgID)
	_, ok = GetSavings(orgID, false, nil, engine, term)
	assert.False(t, ok)
}

func TestCache_BackgroundExpiryRemovesEntries(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_TTL", "1")
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_CAPACITY", "10")
	ResetForTest()

	orgID := "org-expiry"

	Put(orgID, false, nil, sampleSummary(1))
	Put(orgID+"-2", false, nil, sampleSummary(2))
	c := getFleetCache()
	require.Equal(t, 2, c.Len())

	time.Sleep(1100 * time.Millisecond)

	_, ok := Get(orgID, false, nil)
	assert.False(t, ok)
	_, ok = Get(orgID+"-2", false, nil)
	assert.False(t, ok)

	assert.Equal(t, 0, c.Len())

	for i := 0; i < 5; i++ {
		Put(orgID, false, nil, sampleSummary(i))
	}
	assert.Equal(t, 1, c.Len())
}

func TestCache_UsesConfiguredCapacity(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_CAPACITY", "3")
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_TTL", "3600")
	ResetForTest()

	for i := 0; i < 5; i++ {
		Put(fmt.Sprintf("org-cap-%d", i), false, nil, sampleSummary(i))
	}
	c := getFleetCache()
	assert.Equal(t, 3, c.Len())
	assert.Equal(t, float64(3), gaugeValue(t, "rosocp_fleet_summary_cache_size"))
}

func counterValue(t *testing.T, name string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			return m.GetCounter().GetValue()
		}
	}
	return 0
}

func gaugeValue(t *testing.T, name string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			return m.GetGauge().GetValue()
		}
	}
	return 0
}
