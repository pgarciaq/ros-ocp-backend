package costdata

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoundedCostCache_unitEvictionAndTTL(t *testing.T) {
	cache := newBoundedCostCache(2)
	data := &ClusterCostData{ClusterID: "c1", Currency: "USD"}

	cache.put("a", data, time.Hour)
	cache.put("b", data, time.Hour)
	require.Equal(t, 2, cache.len())

	cache.put("c", data, time.Hour)
	assert.Equal(t, 2, cache.len())
	_, ok := cache.get("a")
	assert.False(t, ok, "oldest entry should be evicted")

	cache.put("d", data, 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	_, ok = cache.get("d")
	assert.False(t, ok, "expired entry should miss on access")
}

func TestBoundedCostCache_hitAfterRefresh(t *testing.T) {
	cache := newBoundedCostCache(10)
	first := &ClusterCostData{ClusterID: "c1", Currency: "USD"}
	second := &ClusterCostData{ClusterID: "c1", Currency: "EUR"}

	cache.put("key", first, time.Hour)
	got, ok := cache.get("key")
	require.True(t, ok)
	assert.Equal(t, "USD", got.Currency)

	cache.put("key", second, time.Hour)
	got, ok = cache.get("key")
	require.True(t, ok)
	assert.Equal(t, "EUR", got.Currency)
}
