package middleware

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

const defaultRBACCacheMaxEntries = 500

type rbacCacheEntry struct {
	key         string
	permissions map[string][]string
	expiresAt   time.Time
}

var (
	rbacCache     *boundedRBACCache
	rbacCacheOnce sync.Once

	rbacCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "rosocp_rbac_cache_size",
		Help: "Current number of entries in the RBAC permission LRU cache",
	})

	rbacCacheEvictions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_rbac_cache_evictions_total",
		Help: "Total number of RBAC cache entries evicted due to LRU capacity",
	})
)

type boundedRBACCache struct {
	mu      sync.Mutex
	maxSize int
	items   map[string]*list.Element
	order   *list.List
}

func rbacCacheMaxEntries() int {
	maxEntries := defaultRBACCacheMaxEntries
	if cfg := config.GetConfig(); cfg != nil && cfg.RBACCacheMaxEntries > 0 {
		maxEntries = cfg.RBACCacheMaxEntries
	}
	return maxEntries
}

func getRBACCache() *boundedRBACCache {
	rbacCacheOnce.Do(func() {
		rbacCache = newBoundedRBACCache(rbacCacheMaxEntries())
	})
	return rbacCache
}

func newBoundedRBACCache(maxSize int) *boundedRBACCache {
	if maxSize <= 0 {
		maxSize = defaultRBACCacheMaxEntries
	}
	return &boundedRBACCache{
		maxSize: maxSize,
		items:   make(map[string]*list.Element),
		order:   list.New(),
	}
}

func (c *boundedRBACCache) get(key string) (map[string][]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return nil, false
	}
	entry := elem.Value.(*rbacCacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.removeElement(elem)
		rbacCacheSize.Set(float64(len(c.items)))
		return nil, false
	}
	c.order.MoveToFront(elem)
	return entry.permissions, true
}

func (c *boundedRBACCache) put(key string, permissions map[string][]string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*rbacCacheEntry)
		entry.permissions = permissions
		entry.expiresAt = time.Now().Add(ttl)
		c.order.MoveToFront(elem)
		rbacCacheSize.Set(float64(len(c.items)))
		return
	}

	entry := &rbacCacheEntry{
		key:         key,
		permissions: permissions,
		expiresAt:   time.Now().Add(ttl),
	}
	elem := c.order.PushFront(entry)
	c.items[key] = elem

	for len(c.items) > c.maxSize {
		c.evictOldest()
	}
	rbacCacheSize.Set(float64(len(c.items)))
}

func (c *boundedRBACCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element)
	c.order.Init()
	rbacCacheSize.Set(0)
}

func (c *boundedRBACCache) removeElement(elem *list.Element) {
	entry := elem.Value.(*rbacCacheEntry)
	delete(c.items, entry.key)
	c.order.Remove(elem)
}

func (c *boundedRBACCache) evictOldest() {
	elem := c.order.Back()
	if elem == nil {
		return
	}
	c.removeElement(elem)
	rbacCacheEvictions.Inc()
}

func rbacIdentityCacheKey(encodedIdentity string) string {
	if len(encodedIdentity) <= 32 {
		return encodedIdentity
	}
	sum := sha256.Sum256([]byte(encodedIdentity))
	return hex.EncodeToString(sum[:16])
}

func getCachedRBACPermissions(cacheKey string) (map[string][]string, bool) {
	return getRBACCache().get(cacheKey)
}

func storeCachedRBACPermissions(cacheKey string, permissions map[string][]string, ttl time.Duration) {
	if ttl <= 0 || permissions == nil {
		return
	}
	// Store a shallow copy so callers cannot mutate cached maps.
	copied := make(map[string][]string, len(permissions))
	for k, v := range permissions {
		copied[k] = append([]string(nil), v...)
	}
	getRBACCache().put(cacheKey, copied, ttl)
}

// ClearRBACPermissionCacheForTest removes all cached RBAC entries.
func ClearRBACPermissionCacheForTest() {
	rbacCacheOnce = sync.Once{}
	rbacCache = nil
	rbacCacheSize.Set(0)
}
