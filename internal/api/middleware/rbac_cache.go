package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

const defaultRBACCacheMaxEntries = 500

var (
	rbacCache     *expirable.LRU[string, map[string][]string]
	rbacCacheOnce sync.Once

	rbacCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "rosocp_rbac_cache_size",
		Help: "Current number of entries in the RBAC permission LRU cache",
	})

	rbacCacheRemovals = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_rbac_cache_removals_total",
		Help: "Total number of RBAC cache entries removed (LRU eviction or TTL expiry)",
	})
)

func rbacCacheTTL() time.Duration {
	if cfg := config.GetConfig(); cfg != nil && cfg.RBACCacheTTLSecs > 0 {
		return time.Duration(cfg.RBACCacheTTLSecs) * time.Second
	}
	return 60 * time.Second
}

func rbacCacheMaxEntries() int {
	maxEntries := defaultRBACCacheMaxEntries
	if cfg := config.GetConfig(); cfg != nil && cfg.RBACCacheMaxEntries > 0 {
		maxEntries = cfg.RBACCacheMaxEntries
	}
	return maxEntries
}

func getRBACCache() *expirable.LRU[string, map[string][]string] {
	rbacCacheOnce.Do(func() {
		rbacCache = expirable.NewLRU[string, map[string][]string](
			rbacCacheMaxEntries(),
			func(_ string, _ map[string][]string) {
				rbacCacheRemovals.Inc()
			},
			rbacCacheTTL(),
		)
	})
	return rbacCache
}

func rbacIdentityCacheKey(encodedIdentity string) string {
	if len(encodedIdentity) <= 32 {
		return encodedIdentity
	}
	sum := sha256.Sum256([]byte(encodedIdentity))
	return hex.EncodeToString(sum[:16])
}

func getCachedRBACPermissions(cacheKey string) (map[string][]string, bool) {
	c := getRBACCache()
	perms, ok := c.Get(cacheKey)
	if ok {
		rbacCacheSize.Set(float64(c.Len()))
	}
	return perms, ok
}

func storeCachedRBACPermissions(cacheKey string, permissions map[string][]string) {
	if permissions == nil {
		return
	}
	copied := make(map[string][]string, len(permissions))
	for k, v := range permissions {
		copied[k] = append([]string(nil), v...)
	}
	c := getRBACCache()
	c.Add(cacheKey, copied)
	rbacCacheSize.Set(float64(c.Len()))
}

// ClearRBACPermissionCacheForTest replaces the RBAC cache with a fresh instance for test isolation.
func ClearRBACPermissionCacheForTest() {
	rbacCacheOnce = sync.Once{}
	rbacCache = expirable.NewLRU[string, map[string][]string](
		rbacCacheMaxEntries(),
		func(_ string, _ map[string][]string) {
			rbacCacheRemovals.Inc()
		},
		rbacCacheTTL(),
	)
	rbacCacheSize.Set(0)
}
