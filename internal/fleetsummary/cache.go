package fleetsummary

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/cache"
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
	Currency                string                `json:"currency"`
	EstimatedMonthlySavings money.MoneyAmount     `json:"estimated_monthly_savings"`
	ByCluster               []CachedClusterSavings `json:"by_cluster"`
	ByPlugin                CachedSavingsByPlugin  `json:"by_plugin"`
	GPUSavingsNote          string                `json:"gpu_savings_note,omitempty"`
}

const defaultFleetCacheMaxEntries = 256

var (
	fleetCache       *expirable.LRU[string, CachedSummary]
	savingsCache     *expirable.LRU[string, CachedSavingsSummary]
	fleetCacheOnce   sync.Once
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

	fleetCacheRemovals = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_fleet_summary_cache_removals_total",
		Help: "Fleet summary cache entries removed (LRU eviction or TTL expiry)",
	})

	fleetCacheInvalidations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_fleet_summary_cache_invalidations_total",
		Help: "Explicit fleet summary cache invalidations (InvalidateOrg)",
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

	savingsCacheRemovals = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_savings_summary_cache_removals_total",
		Help: "Savings summary cache entries removed (LRU eviction or TTL expiry)",
	})

	savingsCacheInvalidations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_savings_summary_cache_invalidations_total",
		Help: "Explicit savings summary cache invalidations (InvalidateOrg)",
	})
)

func fleetCacheMaxEntries() int {
	maxEntries := defaultFleetCacheMaxEntries
	if cfg := config.GetConfig(); cfg != nil && cfg.FleetSummaryCacheMaxEntries > 0 {
		maxEntries = cfg.FleetSummaryCacheMaxEntries
	}
	return maxEntries
}

func cacheTTL() time.Duration {
	cfg := config.GetConfig()
	if cfg != nil && cfg.FleetSummaryCacheTTLSecs > 0 {
		return time.Duration(cfg.FleetSummaryCacheTTLSecs) * time.Second
	}
	return 5 * time.Minute
}

func getFleetCache() *expirable.LRU[string, CachedSummary] {
	fleetCacheOnce.Do(func() {
		fleetCache = expirable.NewLRU[string, CachedSummary](
			fleetCacheMaxEntries(),
			func(_ string, _ CachedSummary) {
				fleetCacheRemovals.Inc()
			},
			cacheTTL(),
		)
	})
	return fleetCache
}

func getSavingsCache() *expirable.LRU[string, CachedSavingsSummary] {
	savingsCacheOnce.Do(func() {
		savingsCache = expirable.NewLRU[string, CachedSavingsSummary](
			fleetCacheMaxEntries(),
			func(_ string, _ CachedSavingsSummary) {
				savingsCacheRemovals.Inc()
			},
			cacheTTL(),
		)
	})
	return savingsCache
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

	val, ok := c.Get(key)
	if !ok {
		fleetCacheMisses.Inc()
		return CachedSummary{}, false
	}
	fleetCacheHits.Inc()
	fleetCacheSize.Set(float64(c.Len()))
	return val, true
}

// Put stores a fleet summary in the LRU cache.
func Put(orgID string, rbacScoped bool, userPerms map[string][]string, summary CachedSummary) {
	key := CacheKey(orgID, rbacScoped, userPerms)
	c := getFleetCache()
	c.Add(key, summary)
	fleetCacheSize.Set(float64(c.Len()))
}

// GetSavings returns a cached savings summary when present and not expired.
func GetSavings(orgID string, rbacScoped bool, userPerms map[string][]string, engineProfile, termProfile string) (CachedSavingsSummary, bool) {
	key := SavingsCacheKey(orgID, rbacScoped, userPerms, engineProfile, termProfile)
	c := getSavingsCache()

	val, ok := c.Get(key)
	if !ok {
		savingsCacheMisses.Inc()
		return CachedSavingsSummary{}, false
	}
	savingsCacheHits.Inc()
	savingsCacheSize.Set(float64(c.Len()))
	return val, true
}

// PutSavings stores a savings summary in the LRU cache.
func PutSavings(orgID string, rbacScoped bool, userPerms map[string][]string, engineProfile, termProfile string, summary CachedSavingsSummary) {
	key := SavingsCacheKey(orgID, rbacScoped, userPerms, engineProfile, termProfile)
	c := getSavingsCache()
	c.Add(key, summary)
	savingsCacheSize.Set(float64(c.Len()))
}

// InvalidateOrg drops cached fleet and savings summaries for an org (e.g. after recommendation ingest or settings change).
func InvalidateOrg(orgID string) {
	if orgID == "" {
		return
	}
	prefix := orgID + ":"

	fc := getFleetCache()
	cache.RemoveByPrefix(fc, prefix)
	fleetCacheSize.Set(float64(fc.Len()))
	fleetCacheInvalidations.Inc()

	sc := getSavingsCache()
	cache.RemoveByPrefix(sc, prefix)
	savingsCacheSize.Set(float64(sc.Len()))
	savingsCacheInvalidations.Inc()
}

// ResetForTest clears the fleet and savings summary caches between tests.
// The next access lazily creates fresh instances using the current config.
func ResetForTest() {
	fleetCacheOnce = sync.Once{}
	savingsCacheOnce = sync.Once{}
	fleetCache = nil
	savingsCache = nil
	fleetCacheSize.Set(0)
	savingsCacheSize.Set(0)
}
