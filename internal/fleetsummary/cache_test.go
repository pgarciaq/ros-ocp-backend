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

func TestCache_MetricsHitMissEvictionInvalidation(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_CAPACITY", "2")
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_TTL", "3600")
	ResetForTest()

	orgID := "org-metrics"
	beforeHits := counterValue(t, "rosocp_fleet_summary_cache_hits_total")
	beforeMisses := counterValue(t, "rosocp_fleet_summary_cache_misses_total")
	beforeEvictions := counterValue(t, "rosocp_fleet_summary_cache_evictions_total")
	beforeInvalidations := counterValue(t, "rosocp_fleet_summary_cache_invalidations_total")

	_, ok := Get(orgID, false, nil)
	assert.False(t, ok)
	assert.Equal(t, beforeMisses+1, counterValue(t, "rosocp_fleet_summary_cache_misses_total"))

	Put(orgID, false, nil, sampleSummary(1))
	Put(orgID+"b", false, nil, sampleSummary(2))
	Put(orgID+"c", false, nil, sampleSummary(3))

	assert.Equal(t, beforeEvictions+1, counterValue(t, "rosocp_fleet_summary_cache_evictions_total"))
	assert.Equal(t, float64(2), gaugeValue(t, "rosocp_fleet_summary_cache_size"))

	_, ok = Get(orgID+"c", false, nil)
	assert.True(t, ok)
	assert.Equal(t, beforeHits+1, counterValue(t, "rosocp_fleet_summary_cache_hits_total"))

	InvalidateOrg(orgID)
	assert.Equal(t, beforeInvalidations+1, counterValue(t, "rosocp_fleet_summary_cache_invalidations_total"))
}

func TestCache_LazyExpiryRemovesLRUOrder(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_TTL", "1")
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_CAPACITY", "10")
	ResetForTest()

	orgID := "org-expiry"
	beforeLazy := counterValue(t, "rosocp_fleet_summary_cache_lazy_expiry_total")

	Put(orgID, false, nil, sampleSummary(1))
	Put(orgID+"-2", false, nil, sampleSummary(2))
	require.Equal(t, 2, itemCountForTest())
	require.Equal(t, 2, orderLenForTest())

	time.Sleep(1100 * time.Millisecond)

	_, ok := Get(orgID, false, nil)
	assert.False(t, ok)
	_, ok = Get(orgID+"-2", false, nil)
	assert.False(t, ok)

	assert.Equal(t, 0, itemCountForTest())
	assert.Equal(t, 0, orderLenForTest())
	assert.Equal(t, beforeLazy+2, counterValue(t, "rosocp_fleet_summary_cache_lazy_expiry_total"))

	for i := 0; i < 5; i++ {
		Put(orgID, false, nil, sampleSummary(i))
	}
	assert.Equal(t, 1, itemCountForTest())
	assert.Equal(t, 1, orderLenForTest())
	assert.Equal(t, float64(1), gaugeValue(t, "rosocp_fleet_summary_cache_size"))
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
	assert.Equal(t, 3, itemCountForTest())
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
