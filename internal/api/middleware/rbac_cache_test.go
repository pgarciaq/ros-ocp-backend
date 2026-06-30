package middleware

import (
	"testing"
	"time"

	promtest "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestRBACCache_EvictsWhenMaxEntriesExceeded(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_RBAC_CACHE_MAX_ENTRIES", "2")
	t.Setenv("ROS_RBAC_CACHE_TTL", "300")
	ClearRBACPermissionCacheForTest()
	_ = config.GetConfig()

	cache := getRBACCache()

	storeCachedRBACPermissions("key-a", map[string][]string{"openshift.cluster": {"a"}})
	storeCachedRBACPermissions("key-b", map[string][]string{"openshift.cluster": {"b"}})
	beforeRemovals := promtest.ToFloat64(rbacCacheRemovals)

	storeCachedRBACPermissions("key-c", map[string][]string{"openshift.cluster": {"c"}})

	_, okA := getCachedRBACPermissions("key-a")
	_, okB := getCachedRBACPermissions("key-b")
	_, okC := getCachedRBACPermissions("key-c")

	assert.False(t, okA, "oldest entry should be evicted")
	assert.True(t, okB)
	assert.True(t, okC)
	assert.GreaterOrEqual(t, promtest.ToFloat64(rbacCacheRemovals), beforeRemovals+1)
	assert.Equal(t, float64(cache.Len()), promtest.ToFloat64(rbacCacheSize))
}

func TestRBACCache_RespectsTTL(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_RBAC_CACHE_TTL", "1")
	ClearRBACPermissionCacheForTest()

	storeCachedRBACPermissions("ttl-key", map[string][]string{"*": {}})
	_, ok := getCachedRBACPermissions("ttl-key")
	require.True(t, ok)

	time.Sleep(1100 * time.Millisecond)
	_, ok = getCachedRBACPermissions("ttl-key")
	assert.False(t, ok)
}

func TestRBACCacheSizeMetricTracksEntries(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_RBAC_CACHE_TTL", "300")
	ClearRBACPermissionCacheForTest()

	storeCachedRBACPermissions("metric-key", map[string][]string{"openshift.project": {"p1"}})
	assert.Equal(t, float64(1), promtest.ToFloat64(rbacCacheSize))
}
