package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	promtest "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestTriggerSavingsRecalculationAsync_CoalescesOverlappingJobs(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "true")
	resetSavingsRecalcFlightsForTest()

	pool := testutil.SetupTestDB(t)
	orgID := "org-savings-recalc-coalesce"

	var runCount atomic.Int32
	firstRunStarted := make(chan struct{})
	SetSavingsRecalcRunHookForTest(func(oid string, recTypes []string) {
		if runCount.Add(1) == 1 {
			close(firstRunStarted)
		}
		time.Sleep(100 * time.Millisecond)
	})
	defer ClearSavingsRecalcRunHookForTest()

	restore := SetClusterSavingsRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID string, recTypes []string) error {
		return nil
	})
	defer restore()

	for i := 0; i < 5; i++ {
		TriggerSavingsRecalculationAsync(pool, orgID, "", nil)
	}

	<-firstRunStarted
	require.Eventually(t, func() bool {
		return runCount.Load() >= 2
	}, 2*time.Second, 10*time.Millisecond, "expected initial run plus one coalesced follow-up")

	assert.Equal(t, int32(2), runCount.Load(), "rapid triggers should coalesce into at most two runs")
}

func TestTriggerSavingsRecalculationAsync_CoalescedMetricIncrements(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "true")
	resetSavingsRecalcFlightsForTest()

	pool := testutil.SetupTestDB(t)
	orgID := "org-savings-recalc-metric"

	started := make(chan struct{})
	var once sync.Once
	SetSavingsRecalcRunHookForTest(func(oid string, recTypes []string) {
		once.Do(func() { close(started) })
		time.Sleep(150 * time.Millisecond)
	})
	defer ClearSavingsRecalcRunHookForTest()

	restore := SetClusterSavingsRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID string, recTypes []string) error {
		return nil
	})
	defer restore()

	before := promtest.ToFloat64(savingsRecalcCoalescedTotal.WithLabelValues(orgID))

	TriggerSavingsRecalculationAsync(pool, orgID, "", nil)
	<-started
	TriggerSavingsRecalculationAsync(pool, orgID, "", nil)
	TriggerSavingsRecalculationAsync(pool, orgID, "", nil)

	require.Eventually(t, func() bool {
		after := promtest.ToFloat64(savingsRecalcCoalescedTotal.WithLabelValues(orgID))
		return after-before >= 2
	}, 2*time.Second, 10*time.Millisecond)

	assert.InDelta(t, 2, promtest.ToFloat64(savingsRecalcCoalescedTotal.WithLabelValues(orgID))-before, 0)
}

func TestTriggerSavingsRecalcCoalesced_UsesLatestParameters(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "true")
	resetSavingsRecalcFlightsForTest()

	pool := testutil.SetupTestDB(t)
	orgID := "org-savings-recalc-latest"
	ctx := context.Background()

	var mu sync.Mutex
	var lastCluster string
	var lastRecTypes []string
	started := make(chan struct{})
	var once sync.Once

	SetSavingsRecalcRunHookForTest(func(oid string, recTypes []string) {
		once.Do(func() { close(started) })
		time.Sleep(150 * time.Millisecond)
	})
	defer ClearSavingsRecalcRunHookForTest()

	restore := SetClusterSavingsRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID string, recTypes []string) error {
		mu.Lock()
		lastCluster = clusterUUID
		lastRecTypes = append([]string(nil), recTypes...)
		mu.Unlock()
		return nil
	})
	defer restore()

	done := make(chan struct{})
	go func() {
		triggerSavingsRecalcCoalesced(ctx, pool, orgID, "cluster-A", []string{"container"})
		close(done)
	}()

	<-started
	triggerSavingsRecalcCoalesced(ctx, pool, orgID, "cluster-B", []string{"container"})
	triggerSavingsRecalcCoalesced(ctx, pool, orgID, "cluster-C", []string{"node"})
	<-done

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return lastCluster == "cluster-C" && len(lastRecTypes) == 1 && lastRecTypes[0] == "node"
	}, 2*time.Second, 10*time.Millisecond)
}

func TestTriggerSavingsRecalcCoalesced_InvalidatesCacheAfterCompletion(t *testing.T) {
	config.ResetForTest()
	fleetsummary.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "true")
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_TTL", "3600")
	resetSavingsRecalcFlightsForTest()

	pool := testutil.SetupTestDB(t)
	orgID := "org-savings-recalc-post-cache"
	ctx := context.Background()

	recalcDone := make(chan struct{})
	SetSavingsRecalcRunHookForTest(func(oid string, recTypes []string) {
		// Simulate an API read repopulating cache during the recalc window.
		fleetsummary.Put(orgID, false, nil, fleetsummary.CachedSummary{
			TotalContainers: 99,
			Currency:        "USD",
		})
		close(recalcDone)
	})
	defer ClearSavingsRecalcRunHookForTest()

	restore := SetClusterSavingsRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID string, recTypes []string) error {
		return nil
	})
	defer restore()

	triggerSavingsRecalcCoalesced(ctx, pool, orgID, "", []string{"container"})
	<-recalcDone

	_, ok := fleetsummary.Get(orgID, false, nil)
	assert.False(t, ok, "post-recalc invalidation should clear cache repopulated during recalc")
}
