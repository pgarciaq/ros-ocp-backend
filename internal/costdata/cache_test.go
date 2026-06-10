package costdata_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

func TestBoundedCostCache_TTLExpiry(t *testing.T) {
	restore := costdata.ResetCostDataCacheForTest(10)
	defer restore()
	ttlRestore := costdata.SetCostDataCacheTTLForTest(50 * time.Millisecond)
	defer ttlRestore()
	costdata.ClearCostDataCacheForTest()

	var fetchCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetchCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"cluster_id":"c1","currency":"USD","configured_rates":{},"namespace_aggregates":{}}`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	ctx := context.Background()
	start := time.Now().AddDate(0, 0, -7)
	end := time.Now()

	_, err := provider.GetEffectiveRates(ctx, "org1", "cluster-a", start, end)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&fetchCount))

	_, err = provider.GetEffectiveRates(ctx, "org1", "cluster-a", start, end)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&fetchCount), "second call should hit cache")

	time.Sleep(60 * time.Millisecond)

	_, err = provider.GetEffectiveRates(ctx, "org1", "cluster-a", start, end)
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&fetchCount), "expired entry should refetch")
}

func TestBoundedCostCache_LRUEviction(t *testing.T) {
	restore := costdata.ResetCostDataCacheForTest(2)
	defer restore()
	ttlRestore := costdata.SetCostDataCacheTTLForTest(time.Hour)
	defer ttlRestore()
	costdata.ClearCostDataCacheForTest()

	beforeEvictions := counterValue(t, "rosocp_cost_cache_evictions_total")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		cluster := r.URL.Query().Get("cluster_id")
		resp := map[string]any{
			"cluster_id":           cluster,
			"currency":             "USD",
			"configured_rates":     map[string]any{},
			"namespace_aggregates": map[string]any{},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	ctx := context.Background()
	start := time.Now().AddDate(0, 0, -7)
	end := time.Now()

	_, err := provider.GetEffectiveRates(ctx, "org1", "cluster-a", start, end)
	require.NoError(t, err)
	_, err = provider.GetEffectiveRates(ctx, "org1", "cluster-b", start, end)
	require.NoError(t, err)
	_, err = provider.GetEffectiveRates(ctx, "org1", "cluster-c", start, end)
	require.NoError(t, err)

	afterEvictions := counterValue(t, "rosocp_cost_cache_evictions_total")
	assert.Equal(t, beforeEvictions+1, afterEvictions, "adding third entry should evict oldest")

	gauge := gaugeValue(t, "rosocp_cost_cache_size")
	assert.Equal(t, float64(2), gauge)
}

func TestBoundedCostCache_InvalidateByOrg(t *testing.T) {
	restore := costdata.ResetCostDataCacheForTest(10)
	defer restore()
	ttlRestore := costdata.SetCostDataCacheTTLForTest(time.Hour)
	defer ttlRestore()
	costdata.ClearCostDataCacheForTest()

	var fetchCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetchCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"cluster_id":"c1","currency":"USD","configured_rates":{},"namespace_aggregates":{}}`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	ctx := context.Background()
	start := time.Now().AddDate(0, 0, -7)
	end := time.Now()

	_, err := provider.GetEffectiveRates(ctx, "org1", "cluster-a", start, end)
	require.NoError(t, err)
	_, err = provider.GetEffectiveRates(ctx, "org2", "cluster-a", start, end)
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&fetchCount))

	costdata.InvalidateCostDataCache("org1", "")

	_, err = provider.GetEffectiveRates(ctx, "org1", "cluster-a", start, end)
	require.NoError(t, err)
	_, err = provider.GetEffectiveRates(ctx, "org2", "cluster-a", start, end)
	require.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&fetchCount), "only org1 entry should have been invalidated")
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
