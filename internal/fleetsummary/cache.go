package fleetsummary

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

// CachedSummary is the JSON payload cached for GET /recommendations/openshift/fleet-summary.
type CachedSummary struct {
	TotalContainers        int               `json:"total_containers"`
	ActiveContainers       int               `json:"active_containers"`
	IdleContainers         int               `json:"idle_containers"`
	AbandonedContainers    int               `json:"abandoned_containers"`
	TotalMonthlySavings money.MoneyAmount `json:"total_monthly_savings"`
	ClusterCount           int               `json:"cluster_count"`
	Currency               string            `json:"currency"`
}

type cacheEntry struct {
	key       string
	data      CachedSummary
	expiresAt time.Time
}

type cache struct {
	mu      sync.RWMutex
	maxSize int
	items   map[string]*cacheEntry
	order   []string
}

var fleetCache = newCache(256)

func newCache(maxSize int) *cache {
	if maxSize <= 0 {
		maxSize = 256
	}
	return &cache{
		maxSize: maxSize,
		items:   make(map[string]*cacheEntry),
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
	fleetCache.mu.RLock()
	entry, ok := fleetCache.items[key]
	fleetCache.mu.RUnlock()
	if !ok {
		return CachedSummary{}, false
	}
	if time.Now().After(entry.expiresAt) {
		fleetCache.mu.Lock()
		delete(fleetCache.items, key)
		fleetCache.mu.Unlock()
		return CachedSummary{}, false
	}
	return entry.data, true
}

// Put stores a fleet summary in the LRU cache.
func Put(orgID string, rbacScoped bool, userPerms map[string][]string, summary CachedSummary) {
	key := CacheKey(orgID, rbacScoped, userPerms)
	ttl := cacheTTL()

	fleetCache.mu.Lock()
	defer fleetCache.mu.Unlock()

	if _, ok := fleetCache.items[key]; ok {
		for i, k := range fleetCache.order {
			if k == key {
				fleetCache.order = append(fleetCache.order[:i], fleetCache.order[i+1:]...)
				break
			}
		}
	}
	fleetCache.items[key] = &cacheEntry{
		key:       key,
		data:      summary,
		expiresAt: time.Now().Add(ttl),
	}
	fleetCache.order = append([]string{key}, fleetCache.order...)

	for len(fleetCache.items) > fleetCache.maxSize {
		oldest := fleetCache.order[len(fleetCache.order)-1]
		fleetCache.order = fleetCache.order[:len(fleetCache.order)-1]
		delete(fleetCache.items, oldest)
	}
}

// InvalidateOrg drops cached fleet summaries for an org (e.g. after recommendation ingest).
func InvalidateOrg(orgID string) {
	if orgID == "" {
		return
	}
	prefix := orgID + ":"
	fleetCache.mu.Lock()
	defer fleetCache.mu.Unlock()
	for key := range fleetCache.items {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(fleetCache.items, key)
		}
	}
	newOrder := make([]string, 0, len(fleetCache.order))
	for _, k := range fleetCache.order {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			continue
		}
		newOrder = append(newOrder, k)
	}
	fleetCache.order = newOrder
}

// ResetForTest clears the fleet summary cache between tests.
func ResetForTest() {
	fleetCache = newCache(256)
}
