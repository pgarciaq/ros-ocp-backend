package fleetheatmap

import (
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/cache"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
)

const defaultMaxEntries = 128

var (
	heatmapCache     *expirable.LRU[string, any]
	heatmapCacheOnce sync.Once

	cacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_fleet_heatmap_cache_hits_total",
		Help: "Fleet heatmap cache lookups that returned a valid cached entry",
	})

	cacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_fleet_heatmap_cache_misses_total",
		Help: "Fleet heatmap cache lookups that missed or found an expired entry",
	})

	cacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "rosocp_fleet_heatmap_cache_size",
		Help: "Current number of entries in the fleet heatmap LRU cache",
	})

	cacheRemovals = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_fleet_heatmap_cache_removals_total",
		Help: "Fleet heatmap cache entries removed (LRU eviction or TTL expiry)",
	})

	cacheInvalidations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_fleet_heatmap_cache_invalidations_total",
		Help: "Explicit fleet heatmap cache invalidations (InvalidateOrg)",
	})
)

func cacheTTL() time.Duration {
	cfg := config.GetConfig()
	if cfg != nil && cfg.FleetSummaryCacheTTLSecs > 0 {
		return time.Duration(cfg.FleetSummaryCacheTTLSecs) * time.Second
	}
	return 5 * time.Minute
}

func getCache() *expirable.LRU[string, any] {
	heatmapCacheOnce.Do(func() {
		maxEntries := defaultMaxEntries
		if cfg := config.GetConfig(); cfg != nil && cfg.FleetHeatmapCacheMaxEntries > 0 {
			maxEntries = cfg.FleetHeatmapCacheMaxEntries
		}
		heatmapCache = expirable.NewLRU[string, any](
			maxEntries,
			func(_ string, _ any) { cacheRemovals.Inc() },
			cacheTTL(),
		)
	})
	return heatmapCache
}

// CacheKey builds a cache key for heatmap responses using the fleetsummary RBAC-aware base key.
func CacheKey(orgID string, rbacScoped bool, userPerms map[string][]string, metric, term, engine, clusterFilter string) string {
	key := fleetsummary.CacheKey(orgID, rbacScoped, userPerms) + ":heatmap:" + metric + ":" + term + ":" + engine
	if clusterFilter != "" {
		key += ":cluster=" + clusterFilter
	}
	return key
}

// Get returns a cached heatmap response if present and not expired.
func Get(key string) (any, bool) {
	c := getCache()
	val, ok := c.Get(key)
	if !ok {
		cacheMisses.Inc()
		return nil, false
	}
	cacheHits.Inc()
	cacheSize.Set(float64(c.Len()))
	return val, true
}

// Put stores a heatmap response in the LRU cache.
func Put(key string, resp any) {
	c := getCache()
	c.Add(key, resp)
	cacheSize.Set(float64(c.Len()))
}

// InvalidateOrg drops cached heatmap entries for an org.
func InvalidateOrg(orgID string) {
	if orgID == "" {
		return
	}
	c := getCache()
	cache.RemoveByPrefix(c, orgID+":")
	cacheSize.Set(float64(c.Len()))
	cacheInvalidations.Inc()
}

// ResetForTest clears the heatmap cache between tests.
func ResetForTest() {
	heatmapCacheOnce = sync.Once{}
	heatmapCache = nil
	cacheSize.Set(0)
}
