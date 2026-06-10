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
	ClearRBACPermissionCacheForTest()
	_ = config.GetConfig()

	cache := getRBACCache()
	cache.maxSize = 2

	storeCachedRBACPermissions("key-a", map[string][]string{"openshift.cluster": {"a"}}, time.Minute)
	storeCachedRBACPermissions("key-b", map[string][]string{"openshift.cluster": {"b"}}, time.Minute)
	beforeEvictions := promtest.ToFloat64(rbacCacheEvictions)

	storeCachedRBACPermissions("key-c", map[string][]string{"openshift.cluster": {"c"}}, time.Minute)

	_, okA := getCachedRBACPermissions("key-a")
	_, okB := getCachedRBACPermissions("key-b")
	_, okC := getCachedRBACPermissions("key-c")

	assert.False(t, okA, "oldest entry should be evicted")
	assert.True(t, okB)
	assert.True(t, okC)
	assert.Equal(t, beforeEvictions+1, promtest.ToFloat64(rbacCacheEvictions))
	assert.Equal(t, float64(2), promtest.ToFloat64(rbacCacheSize))
}

func TestRBACCache_RespectsTTL(t *testing.T) {
	ClearRBACPermissionCacheForTest()

	storeCachedRBACPermissions("ttl-key", map[string][]string{"*": {}}, 20*time.Millisecond)
	_, ok := getCachedRBACPermissions("ttl-key")
	require.True(t, ok)

	time.Sleep(30 * time.Millisecond)
	_, ok = getCachedRBACPermissions("ttl-key")
	assert.False(t, ok)
}

func TestRBACCacheSizeMetricTracksEntries(t *testing.T) {
	ClearRBACPermissionCacheForTest()

	storeCachedRBACPermissions("metric-key", map[string][]string{"openshift.project": {"p1"}}, time.Minute)
	assert.Equal(t, float64(1), promtest.ToFloat64(rbacCacheSize))
}
