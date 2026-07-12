package clustercache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
)

const defaultMaxEntries = 256
const defaultTTLSecs = 30

var (
	clusterCache     *expirable.LRU[string, []string]
	clusterCacheOnce sync.Once

	cacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_cluster_cache_hits_total",
		Help: "Cluster UUID cache lookups that returned a valid cached entry",
	})

	cacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_cluster_cache_misses_total",
		Help: "Cluster UUID cache lookups that missed or found an expired entry",
	})

	cacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "rosocp_cluster_cache_size",
		Help: "Current number of entries in the cluster UUID LRU cache",
	})

	cacheRemovals = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_cluster_cache_removals_total",
		Help: "Cluster UUID cache entries removed (LRU eviction or TTL expiry)",
	})

	cacheInvalidations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_cluster_cache_invalidations_total",
		Help: "Explicit cluster UUID cache invalidations (InvalidateOrg)",
	})
)

func cacheTTL() time.Duration {
	cfg := config.GetConfig()
	if cfg != nil && cfg.ClusterCacheTTLSecs > 0 {
		return time.Duration(cfg.ClusterCacheTTLSecs) * time.Second
	}
	return defaultTTLSecs * time.Second
}

func maxEntries() int {
	if cfg := config.GetConfig(); cfg != nil && cfg.ClusterCacheMaxEntries > 0 {
		return cfg.ClusterCacheMaxEntries
	}
	return defaultMaxEntries
}

func getCache() *expirable.LRU[string, []string] {
	clusterCacheOnce.Do(func() {
		clusterCache = expirable.NewLRU[string, []string](
			maxEntries(),
			func(_ string, _ []string) { cacheRemovals.Inc() },
			cacheTTL(),
		)
	})
	return clusterCache
}

// GetClustersForOrg returns the cluster UUIDs for an org, using the cache when available.
// On cache miss, queries the database and stores the result.
func GetClustersForOrg(ctx context.Context, orgID string) ([]string, error) {
	c := getCache()
	if val, ok := c.Get(orgID); ok {
		cacheHits.Inc()
		cacheSize.Set(float64(c.Len()))
		return val, nil
	}
	cacheMisses.Inc()

	pool := db.GetPool()
	if pool == nil {
		return nil, fmt.Errorf("no database pool")
	}
	return fetchAndCache(ctx, pool, orgID)
}

// GetClustersForOrgWithPool is like GetClustersForOrg but accepts an explicit pool,
// useful when the caller already holds a pool reference.
func GetClustersForOrgWithPool(ctx context.Context, pool *pgxpool.Pool, orgID string) ([]string, error) {
	c := getCache()
	if val, ok := c.Get(orgID); ok {
		cacheHits.Inc()
		cacheSize.Set(float64(c.Len()))
		return val, nil
	}
	cacheMisses.Inc()

	if pool == nil {
		return nil, fmt.Errorf("no database pool")
	}
	return fetchAndCache(ctx, pool, orgID)
}

func fetchAndCache(ctx context.Context, pool *pgxpool.Pool, orgID string) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT DISTINCT c.cluster_uuid::text
		 FROM clusters c
		 JOIN rh_accounts a ON c.tenant_id = a.id
		 WHERE a.org_id = $1`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var uuids []string
	for rows.Next() {
		var uuid string
		if err := rows.Scan(&uuid); err != nil {
			return nil, err
		}
		uuids = append(uuids, uuid)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	c := getCache()
	c.Add(orgID, uuids)
	cacheSize.Set(float64(c.Len()))
	return uuids, nil
}

// InvalidateOrg removes the cached cluster list for a specific org.
func InvalidateOrg(orgID string) {
	if orgID == "" {
		return
	}
	c := getCache()
	c.Remove(orgID)
	cacheSize.Set(float64(c.Len()))
	cacheInvalidations.Inc()
}

// ResetForTest clears the cluster cache between tests.
func ResetForTest() {
	clusterCacheOnce = sync.Once{}
	clusterCache = nil
	cacheSize.Set(0)
}
