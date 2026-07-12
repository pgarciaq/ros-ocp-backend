package clustercache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func counterValue(t *testing.T, name string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			return m.GetCounter().GetValue()
		}
	}
	return 0
}

func gaugeValue(t *testing.T, name string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			return m.GetGauge().GetValue()
		}
	}
	return 0
}

func TestClusterCache_HitOnSecondCall(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_CLUSTER_CACHE_TTL", "300")
	t.Setenv("ROS_CLUSTER_CACHE_CAPACITY", "256")
	ResetForTest()

	orgID := "org-hit-test"
	clusters := []string{"uuid-1", "uuid-2", "uuid-3"}

	c := getCache()
	c.Add(orgID, clusters)
	cacheSize.Set(float64(c.Len()))

	beforeHits := counterValue(t, "rosocp_cluster_cache_hits_total")

	// GetClustersForOrgWithPool should return the cached value without querying DB.
	// Pass a nil pool — if the cache hits, the pool is never used.
	got, err := GetClustersForOrgWithPool(context.Background(), (*pgxpool.Pool)(nil), orgID)
	require.NoError(t, err)
	assert.Equal(t, clusters, got)
	assert.Equal(t, beforeHits+1, counterValue(t, "rosocp_cluster_cache_hits_total"))
}

func TestClusterCache_TTLExpiry(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_CLUSTER_CACHE_TTL", "1")
	t.Setenv("ROS_CLUSTER_CACHE_CAPACITY", "10")
	ResetForTest()

	orgID := "org-ttl-test"
	clusters := []string{"uuid-a", "uuid-b"}

	c := getCache()
	c.Add(orgID, clusters)
	cacheSize.Set(float64(c.Len()))
	require.Equal(t, 1, c.Len())

	time.Sleep(1100 * time.Millisecond)

	// After TTL expiry, cache miss — GetClustersForOrgWithPool with nil pool should error
	// because the entry expired and it tries to query.
	beforeMisses := counterValue(t, "rosocp_cluster_cache_misses_total")
	_, err := GetClustersForOrgWithPool(context.Background(), (*pgxpool.Pool)(nil), orgID)
	assert.Error(t, err)
	assert.Equal(t, beforeMisses+1, counterValue(t, "rosocp_cluster_cache_misses_total"))
}

func TestClusterCache_InvalidateOrg(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_CLUSTER_CACHE_TTL", "300")
	t.Setenv("ROS_CLUSTER_CACHE_CAPACITY", "256")
	ResetForTest()

	orgA := "org-invalidate-a"
	orgB := "org-invalidate-b"
	clustersA := []string{"uuid-1"}
	clustersB := []string{"uuid-2", "uuid-3"}

	c := getCache()
	c.Add(orgA, clustersA)
	c.Add(orgB, clustersB)
	cacheSize.Set(float64(c.Len()))
	require.Equal(t, 2, c.Len())

	beforeInv := counterValue(t, "rosocp_cluster_cache_invalidations_total")

	InvalidateOrg(orgA)

	assert.Equal(t, beforeInv+1, counterValue(t, "rosocp_cluster_cache_invalidations_total"))
	assert.Equal(t, 1, c.Len())

	// orgA is gone, orgB still present
	_, err := GetClustersForOrgWithPool(context.Background(), (*pgxpool.Pool)(nil), orgA)
	assert.Error(t, err) // miss → tries nil pool → error

	got, err := GetClustersForOrgWithPool(context.Background(), (*pgxpool.Pool)(nil), orgB)
	require.NoError(t, err)
	assert.Equal(t, clustersB, got)
}

func TestClusterCache_MutationSafety(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_CLUSTER_CACHE_TTL", "300")
	t.Setenv("ROS_CLUSTER_CACHE_CAPACITY", "256")
	ResetForTest()

	orgID := "org-mutation-test"
	original := []string{"uuid-1", "uuid-2", "uuid-3"}

	c := getCache()
	c.Add(orgID, append([]string(nil), original...))
	cacheSize.Set(float64(c.Len()))

	// First read
	got1, err := GetClustersForOrgWithPool(context.Background(), (*pgxpool.Pool)(nil), orgID)
	require.NoError(t, err)
	assert.Equal(t, original, got1)

	// Mutate the returned slice: overwrite, append, and sort
	got1[0] = "corrupted"
	got1 = append(got1, "extra-uuid")

	// Second read must return the original, unmodified data
	got2, err := GetClustersForOrgWithPool(context.Background(), (*pgxpool.Pool)(nil), orgID)
	require.NoError(t, err)
	assert.Equal(t, original, got2)
}

func TestClusterCache_EmptySlice(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_CLUSTER_CACHE_TTL", "300")
	t.Setenv("ROS_CLUSTER_CACHE_CAPACITY", "256")
	ResetForTest()

	orgID := "org-empty-test"

	c := getCache()
	c.Add(orgID, []string{})
	cacheSize.Set(float64(c.Len()))

	got, err := GetClustersForOrgWithPool(context.Background(), (*pgxpool.Pool)(nil), orgID)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestClusterCache_CapacityEviction(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_CLUSTER_CACHE_TTL", "300")
	t.Setenv("ROS_CLUSTER_CACHE_CAPACITY", "3")
	ResetForTest()

	for i := 0; i < 5; i++ {
		c := getCache()
		c.Add(fmt.Sprintf("org-%d", i), []string{fmt.Sprintf("uuid-%d", i)})
		cacheSize.Set(float64(c.Len()))
	}
	c := getCache()
	assert.Equal(t, 3, c.Len())
}
