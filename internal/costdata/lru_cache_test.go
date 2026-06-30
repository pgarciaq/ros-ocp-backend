package costdata

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCostCache_EvictionAndTTL(t *testing.T) {
	restore := ResetCostDataCacheForTest(2)
	defer restore()

	data := &ClusterCostData{ClusterID: "c1", Currency: "USD"}

	c := currentCostCache()
	c.Add(costCacheKey("org1", "c1"), data)
	c.Add(costCacheKey("org1", "c2"), data)
	require.Equal(t, 2, c.Len())

	c.Add(costCacheKey("org1", "c3"), data)
	assert.Equal(t, 2, c.Len())
	_, ok := c.Get(costCacheKey("org1", "c1"))
	assert.False(t, ok, "oldest entry should be evicted")
}

func TestCostCache_TTLExpiry(t *testing.T) {
	restore := SetCostDataCacheTTLForTest(1 * time.Second)
	defer restore()

	data := &ClusterCostData{ClusterID: "c1", Currency: "USD"}
	c := currentCostCache()
	c.Add("ttl-key", data)
	_, ok := c.Get("ttl-key")
	require.True(t, ok)

	time.Sleep(1100 * time.Millisecond)
	_, ok = c.Get("ttl-key")
	assert.False(t, ok, "expired entry should miss")
}

func TestCostCache_HitAfterRefresh(t *testing.T) {
	restore := ResetCostDataCacheForTest(10)
	defer restore()

	first := &ClusterCostData{ClusterID: "c1", Currency: "USD"}
	second := &ClusterCostData{ClusterID: "c1", Currency: "EUR"}

	c := currentCostCache()
	c.Add("key", first)
	got, ok := c.Get("key")
	require.True(t, ok)
	assert.Equal(t, "USD", got.Currency)

	c.Add("key", second)
	got, ok = c.Get("key")
	require.True(t, ok)
	assert.Equal(t, "EUR", got.Currency)
}

func TestCostCache_InvalidateByPrefix(t *testing.T) {
	restore := ResetCostDataCacheForTest(100)
	defer restore()

	data := &ClusterCostData{ClusterID: "c1", Currency: "USD"}
	c := currentCostCache()
	c.Add(costCacheKey("org1", "c1"), data)
	c.Add(costCacheKey("org1", "c2"), data)
	c.Add(costCacheKey("org2", "c1"), data)

	InvalidateCostDataCache("org1", "")
	assert.Equal(t, 1, c.Len())
	_, ok := c.Get(costCacheKey("org2", "c1"))
	assert.True(t, ok)
}
