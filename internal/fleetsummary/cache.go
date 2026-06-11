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
// ADR-0112 pattern: bounded LRU+TTL in-memory cache for API hot paths.
type CachedSummary struct {
	TotalContainers     int               `json:"total_containers"`
	ActiveContainers    int               `json:"active_containers"`
	IdleContainers      int               `json:"idle_containers"`
	AbandonedContainers int               `json:"abandoned_containers"`
	TotalMonthlySavings money.MoneyAmount `json:"total_monthly_savings"`
	ClusterCount        int               `json:"cluster_count"`
	Currency            string            `json:"currency"`
}

// CachedClusterSavings is one cluster row in a cached savings summary.
type CachedClusterSavings struct {
	ClusterUUID             string            `json:"cluster_uuid"`
	ClusterAlias            string            `json:"cluster_alias"`
	EstimatedMonthlySavings money.MoneyAmount `json:"estimated_monthly_savings"`
	HasCostData             bool              `json:"has_cost_data"`
}

// CachedSavingsByPlugin breaks down cached savings by recommendation plugin.
type CachedSavingsByPlugin struct {
	Container money.MoneyAmount `json:"container"`
	GPU       money.MoneyAmount `json:"gpu"`
	Node      money.MoneyAmount `json:"node"`
	PVC       money.MoneyAmount `json:"pvc"`
	Snapshot  money.MoneyAmount `json:"snapshot"`
	VM        money.MoneyAmount `json:"vm"`
}

// CachedSavingsSummary is the JSON payload cached for GET /recommendations/openshift/savings-summary
// (default rollup only — not group_by variants). ADR-0112: bounded LRU+TTL in-memory cache for API hot paths.
type CachedSavingsSummary struct {
	Currency                string                  `json:"currency"`
	EstimatedMonthlySavings money.MoneyAmount       `json:"estimated_monthly_savings"`
	ByCluster               []CachedClusterSavings  `json:"by_cluster"`
	ByPlugin                CachedSavingsByPlugin   `json:"by_plugin"`
	GPUSavingsNote          string                  `json:"gpu_savings_note,omitempty"`
}

type cacheEntry struct {
	key       string
	data      CachedSummary
	expiresAt time.Time
}

type savingsCacheEntry struct {
	key       string
	data      CachedSavingsSummary
	expiresAt time.Time
}

const defaultFleetCacheMaxEntries = 256

var (
	fleetCache     *boundedFleetCache
	savingsCache   *boundedSavingsCache
	fleetCacheOnce sync.Once
	savingsCacheOnce sync.Once

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

	savingsCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_savings_summary_cache_hits_total",
		Help: "Savings summary cache lookups that returned a valid cached entry",
	})

	savingsCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_savings_summary_cache_misses_total",
		Help: "Savings summary cache lookups that missed or found an expired entry",
	})

	savingsCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "rosocp_savings_summary_cache_size",
		Help: "Current number of entries in the savings summary LRU cache",
	})

	savingsCacheEvictions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_savings_summary_cache_evictions_total",
		Help: "Savings summary cache entries evicted due to LRU capacity",
	})

	savingsCacheInvalidations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_savings_summary_cache_invalidations_total",
		Help: "Explicit savings summary cache invalidations (InvalidateOrg)",
	})

	savingsCacheLazyExpiry = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_savings_summary_cache_lazy_expiry_total",
		Help: "Savings summary cache entries removed on read because TTL expired",
	})
)

type boundedFleetCache struct {
	mu      sync.Mutex
	maxSize int
	items   map[string]*list.Element
	order   *list.List
}

type boundedSavingsCache struct {
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

func getSavingsCache() *boundedSavingsCache {
	savingsCacheOnce.Do(func() {
		savingsCache = newBoundedSavingsCache(fleetCacheMaxEntries())
	})
	return savingsCache
}

func newBoundedSavingsCache(maxSize int) *boundedSavingsCache {
	if maxSize <= 0 {
		maxSize = defaultFleetCacheMaxEntries
	}
	return &boundedSavingsCache{
		maxSize: maxSize,
		items:   make(map[string]*list.Element),
		order:   list.New(),
	}
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

// SavingsCacheKey builds a cache key for savings-summary default rollup responses.
func SavingsCacheKey(orgID string, rbacScoped bool, userPerms map[string][]string, engineProfile, termProfile string) string {
	return CacheKey(orgID, rbacScoped, userPerms) + ":savings:" + engineProfile + ":" + termProfile
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

// GetSavings returns a cached savings summary when present and not expired.
func GetSavings(orgID string, rbacScoped bool, userPerms map[string][]string, engineProfile, termProfile string) (CachedSavingsSummary, bool) {
	key := SavingsCacheKey(orgID, rbacScoped, userPerms, engineProfile, termProfile)
	c := getSavingsCache()

	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		savingsCacheMisses.Inc()
		return CachedSavingsSummary{}, false
	}
	entry := elem.Value.(*savingsCacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.removeElement(elem)
		savingsCacheLazyExpiry.Inc()
		savingsCacheMisses.Inc()
		savingsCacheSize.Set(float64(len(c.items)))
		return CachedSavingsSummary{}, false
	}
	c.order.MoveToFront(elem)
	savingsCacheHits.Inc()
	return entry.data, true
}

// PutSavings stores a savings summary in the LRU cache.
func PutSavings(orgID string, rbacScoped bool, userPerms map[string][]string, engineProfile, termProfile string, summary CachedSavingsSummary) {
	key := SavingsCacheKey(orgID, rbacScoped, userPerms, engineProfile, termProfile)
	ttl := cacheTTL()
	c := getSavingsCache()

	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*savingsCacheEntry)
		entry.data = summary
		entry.expiresAt = time.Now().Add(ttl)
		c.order.MoveToFront(elem)
		savingsCacheSize.Set(float64(len(c.items)))
		return
	}

	entry := &savingsCacheEntry{
		key:       key,
		data:      summary,
		expiresAt: time.Now().Add(ttl),
	}
	elem := c.order.PushFront(entry)
	c.items[key] = elem

	for len(c.items) > c.maxSize {
		c.evictOldest()
	}
	savingsCacheSize.Set(float64(len(c.items)))
}

// InvalidateOrg drops cached fleet and savings summaries for an org (e.g. after recommendation ingest or settings change).
func InvalidateOrg(orgID string) {
	if orgID == "" {
		return
	}
	prefix := orgID + ":"
	fleet := getFleetCache()
	fleet.mu.Lock()
	for key, elem := range fleet.items {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			fleet.removeElement(elem)
		}
	}
	fleetCacheSize.Set(float64(len(fleet.items)))
	fleet.mu.Unlock()

	savings := getSavingsCache()
	savings.mu.Lock()
	for key, elem := range savings.items {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			savings.removeElement(elem)
		}
	}
	savingsCacheSize.Set(float64(len(savings.items)))
	savings.mu.Unlock()

	fleetCacheInvalidations.Inc()
	savingsCacheInvalidations.Inc()
}

// ResetForTest clears the fleet and savings summary caches between tests.
func ResetForTest() {
	fleetCacheOnce = sync.Once{}
	savingsCacheOnce = sync.Once{}
	fleetCache = nil
	savingsCache = nil
	fleetCacheSize.Set(0)
	savingsCacheSize.Set(0)
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

func (c *boundedSavingsCache) removeElement(elem *list.Element) {
	entry := elem.Value.(*savingsCacheEntry)
	delete(c.items, entry.key)
	c.order.Remove(elem)
}

func (c *boundedSavingsCache) evictOldest() {
	elem := c.order.Back()
	if elem == nil {
		return
	}
	c.removeElement(elem)
	savingsCacheEvictions.Inc()
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
