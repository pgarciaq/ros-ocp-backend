package fleetsummary

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

// CachedSummary is the JSON payload cached for GET /recommendations/openshift/fleet-summary.
type CachedSummary struct {
	TotalContainers     int               `json:"total_containers"`
	ActiveContainers    int               `json:"active_containers"`
	IdleContainers      int               `json:"idle_containers"`
	AbandonedContainers int               `json:"abandoned_containers"`
	TotalMonthlySavings money.MoneyAmount `json:"total_monthly_savings"`
	ClusterCount        int               `json:"cluster_count"`
	Currency            string            `json:"currency"`
}

type cacheEntry struct {
	key       string
	data      CachedSummary
	expiresAt time.Time
}

const defaultFleetCacheMaxEntries = 256

var (
	fleetCache     *boundedFleetCache
	fleetCacheOnce sync.Once

	fleetCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "rosocp_fleet_summary_cache_size",
		Help: "Current number of entries in the fleet summary LRU cache",
	})

	fleetCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_fleet_summary_cache_hits_total",
		Help: "Fleet summary cache lookups that returned a valid cached entry",
	})

	fleetCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_fleet_summary_cache_misses_total",
		Help: "Fleet summary cache lookups that missed or found an expired entry",
	})

	fleetCacheEvictions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_fleet_summary_cache_evictions_total",
		Help: "Fleet summary cache entries evicted due to LRU capacity",
	})

	fleetCacheInvalidations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_fleet_summary_cache_invalidations_total",
		Help: "Explicit fleet summary cache invalidations (InvalidateOrg)",
	})

	fleetCacheLazyExpiry = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_fleet_summary_cache_lazy_expiry_total",
		Help: "Fleet summary cache entries removed on read because TTL expired",
	})
)

type boundedFleetCache struct {
	mu      sync.Mutex
	maxSize int
	items   map[string]*list.Element
	order   *list.List
}

func fleetCacheMaxEntries() int {
	maxEntries := defaultFleetCacheMaxEntries
	if cfg := config.GetConfig(); cfg != nil && cfg.FleetSummaryCacheMaxEntries > 0 {
		maxEntries = cfg.FleetSummaryCacheMaxEntries
	}
	return maxEntries
}

func getFleetCache() *boundedFleetCache {
	fleetCacheOnce.Do(func() {
		fleetCache = newBoundedFleetCache(fleetCacheMaxEntries())
	})
	return fleetCache
}

func newBoundedFleetCache(maxSize int) *boundedFleetCache {
	if maxSize <= 0 {
		maxSize = defaultFleetCacheMaxEntries
	}
	return &boundedFleetCache{
		maxSize: maxSize,
		items:   make(map[string]*list.Element),
		order:   list.New(),
	}
}

func cacheTTL() time.Duration {
	cfg := config.GetConfig()
	if cfg != nil && cfg.FleetSummaryCacheTTLSecs > 0 {
		return time.Duration(cfg.FleetSummaryCacheTTLSecs) * time.Second
	}
	return 5 * time.Minute
}

// CacheKey builds a cache key from org_id and optional RBAC scope.
func CacheKey(orgID string, rbacScoped bool, userPerms map[string][]string) string {
	if !rbacScoped {
		return orgID + ":all"
	}
	permsCopy := make(map[string][]string, len(userPerms))
	for k, v := range userPerms {
		cp := append([]string(nil), v...)
		sort.Strings(cp)
		permsCopy[k] = cp
	}
	b, _ := json.Marshal(permsCopy)
	sum := sha256.Sum256(b)
	return orgID + ":rbac:" + hex.EncodeToString(sum[:8])
}

// Get returns a cached fleet summary when present and not expired.
func Get(orgID string, rbacScoped bool, userPerms map[string][]string) (CachedSummary, bool) {
	key := CacheKey(orgID, rbacScoped, userPerms)
	c := getFleetCache()

	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		fleetCacheMisses.Inc()
		return CachedSummary{}, false
	}
	entry := elem.Value.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.removeElement(elem)
		fleetCacheLazyExpiry.Inc()
		fleetCacheMisses.Inc()
		fleetCacheSize.Set(float64(len(c.items)))
		return CachedSummary{}, false
	}
	c.order.MoveToFront(elem)
	fleetCacheHits.Inc()
	return entry.data, true
}

// Put stores a fleet summary in the LRU cache.
func Put(orgID string, rbacScoped bool, userPerms map[string][]string, summary CachedSummary) {
	key := CacheKey(orgID, rbacScoped, userPerms)
	ttl := cacheTTL()
	c := getFleetCache()

	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*cacheEntry)
		entry.data = summary
		entry.expiresAt = time.Now().Add(ttl)
		c.order.MoveToFront(elem)
		fleetCacheSize.Set(float64(len(c.items)))
		return
	}

	entry := &cacheEntry{
		key:       key,
		data:      summary,
		expiresAt: time.Now().Add(ttl),
	}
	elem := c.order.PushFront(entry)
	c.items[key] = elem

	for len(c.items) > c.maxSize {
		c.evictOldest()
	}
	fleetCacheSize.Set(float64(len(c.items)))
}

// InvalidateOrg drops cached fleet summaries for an org (e.g. after recommendation ingest or settings change).
func InvalidateOrg(orgID string) {
	if orgID == "" {
		return
	}
	prefix := orgID + ":"
	c := getFleetCache()

	c.mu.Lock()
	defer c.mu.Unlock()

	for key, elem := range c.items {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			c.removeElement(elem)
		}
	}
	fleetCacheSize.Set(float64(len(c.items)))
	fleetCacheInvalidations.Inc()
}

// ResetForTest clears the fleet summary cache between tests.
func ResetForTest() {
	fleetCacheOnce = sync.Once{}
	fleetCache = nil
	fleetCacheSize.Set(0)
}

func (c *boundedFleetCache) removeElement(elem *list.Element) {
	entry := elem.Value.(*cacheEntry)
	delete(c.items, entry.key)
	c.order.Remove(elem)
}

func (c *boundedFleetCache) evictOldest() {
	elem := c.order.Back()
	if elem == nil {
		return
	}
	c.removeElement(elem)
	fleetCacheEvictions.Inc()
}

// orderLenForTest exposes LRU list length for unit tests in this package.
func orderLenForTest() int {
	c := getFleetCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// itemCountForTest exposes live map size for unit tests in this package.
func itemCountForTest() int {
	c := getFleetCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}
