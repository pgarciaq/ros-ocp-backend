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

	before := promtest.ToFloat64(savingsRecalcCoalescedTotal)

	TriggerSavingsRecalculationAsync(pool, orgID, "", nil)
	<-started
	TriggerSavingsRecalculationAsync(pool, orgID, "", nil)
	TriggerSavingsRecalculationAsync(pool, orgID, "", nil)

	require.Eventually(t, func() bool {
		after := promtest.ToFloat64(savingsRecalcCoalescedTotal)
		return after-before >= 2
	}, 2*time.Second, 10*time.Millisecond)

	assert.InDelta(t, 2, promtest.ToFloat64(savingsRecalcCoalescedTotal)-before, 0)
}

type savingsRecalcCall struct {
	clusterUUID string
	recTypes    []string
}

// TestTriggerSavingsRecalcCoalesced_RunsAllPendingClusters pins #812: while a
// flight runs for cluster A, callbacks for clusters B and C arrive. The flight
// must drain BOTH B and C (each with its own recTypes) — the previous
// single-slot latestSavings let C's arrival overwrite B, leaving B's
// rate-cache entry and savings stale until some later trigger. Koku's
// shared-price-list dispatch fires overlapping per-provider callbacks for
// different clusters, so the burst is reachable in practice.
func TestTriggerSavingsRecalcCoalesced_RunsAllPendingClusters(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "true")
	resetSavingsRecalcFlightsForTest()

	pool := testutil.SetupTestDB(t)
	orgID := "org-savings-recalc-pending-set"
	ctx := context.Background()

	var mu sync.Mutex
	var calls []savingsRecalcCall
	started := make(chan struct{})
	var once sync.Once

	SetSavingsRecalcRunHookForTest(func(oid string, recTypes []string) {
		once.Do(func() { close(started) })
		time.Sleep(150 * time.Millisecond)
	})
	defer ClearSavingsRecalcRunHookForTest()

	restore := SetClusterSavingsRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID string, recTypes []string) error {
		mu.Lock()
		calls = append(calls, savingsRecalcCall{
			clusterUUID: clusterUUID,
			recTypes:    append([]string(nil), recTypes...),
		})
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
		return len(calls) == 3
	}, 2*time.Second, 10*time.Millisecond,
		"the flight must drain all pending clusters; a dropped cluster B means C's arrival overwrote it (#812)")

	mu.Lock()
	defer mu.Unlock()
	byCluster := map[string][]string{}
	for _, c := range calls {
		byCluster[c.clusterUUID] = c.recTypes
	}
	assert.Equal(t, []string{"container"}, byCluster["cluster-A"],
		"cluster A should be recalculated with its own recTypes")
	assert.Equal(t, []string{"container"}, byCluster["cluster-B"],
		"cluster B must be recalculated — it must not be overwritten by cluster C's arrival (#812)")
	assert.Equal(t, []string{"node"}, byCluster["cluster-C"],
		"cluster C should be recalculated with its own recTypes")
}

// TestTriggerSavingsRecalcCoalesced_MergesRecTypesForSameCluster pins the
// coalescing half of #812's fix: same-cluster arrivals during a flight merge
// into ONE trailing run carrying the union of their recTypes — the burst
// coalescing benefit is preserved (no unbounded flight spawning), and no
// recType is lost.
func TestTriggerSavingsRecalcCoalesced_MergesRecTypesForSameCluster(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "true")
	resetSavingsRecalcFlightsForTest()

	pool := testutil.SetupTestDB(t)
	orgID := "org-savings-recalc-merge"
	ctx := context.Background()

	var mu sync.Mutex
	var calls []savingsRecalcCall
	started := make(chan struct{})
	var once sync.Once

	SetSavingsRecalcRunHookForTest(func(oid string, recTypes []string) {
		once.Do(func() { close(started) })
		time.Sleep(150 * time.Millisecond)
	})
	defer ClearSavingsRecalcRunHookForTest()

	restore := SetClusterSavingsRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID string, recTypes []string) error {
		mu.Lock()
		calls = append(calls, savingsRecalcCall{
			clusterUUID: clusterUUID,
			recTypes:    append([]string(nil), recTypes...),
		})
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
	triggerSavingsRecalcCoalesced(ctx, pool, orgID, "cluster-B", []string{"node"})
	<-done

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(calls) == 2
	}, 2*time.Second, 10*time.Millisecond,
		"A once plus the coalesced B once — same-cluster arrivals must not spawn extra runs")

	mu.Lock()
	defer mu.Unlock()
	var bCalls []savingsRecalcCall
	for _, c := range calls {
		if c.clusterUUID == "cluster-B" {
			bCalls = append(bCalls, c)
		}
	}
	require.Len(t, bCalls, 1,
		"same-cluster arrivals during a flight should coalesce into one trailing run")
	assert.ElementsMatch(t, []string{"container", "node"}, bCalls[0].recTypes,
		"the coalesced run must carry the union of both arrivals' recTypes — dropping an earlier arrival's types leaves its recommendations stale")
}

// TestTriggerSavingsRecalcCoalesced_ArrivalDuringTrailingRunIsDrained pins
// the drain loop's re-check: an arrival landing while a TRAILING run executes
// (not just during the first run) must be picked up by the next loop pass. A
// "snapshot the pending set once, run it, exit" shortcut would drop it. This
// behavior already holds on the pre-#812 single-slot guard (its loop also
// re-checks), so this test guards a mechanism the fix must not regress.
func TestTriggerSavingsRecalcCoalesced_ArrivalDuringTrailingRunIsDrained(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "true")
	resetSavingsRecalcFlightsForTest()

	pool := testutil.SetupTestDB(t)
	orgID := "org-savings-recalc-trailing-arrival"
	ctx := context.Background()

	var mu sync.Mutex
	var calls []savingsRecalcCall
	started := make(chan struct{})
	var once sync.Once
	var runCount atomic.Int32

	SetSavingsRecalcRunHookForTest(func(oid string, recTypes []string) {
		switch runCount.Add(1) {
		case 1:
			once.Do(func() { close(started) })
			time.Sleep(150 * time.Millisecond)
		case 2:
			// Cluster B's run IS the trailing run. Fire D's arrival now,
			// from inside the run: the drain loop must come back for it.
			// (Safe: the flight mutex is not held during a recalc.)
			triggerSavingsRecalcCoalesced(ctx, pool, orgID, "cluster-D", []string{"node"})
			time.Sleep(150 * time.Millisecond)
		}
	})
	defer ClearSavingsRecalcRunHookForTest()

	restore := SetClusterSavingsRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID string, recTypes []string) error {
		mu.Lock()
		calls = append(calls, savingsRecalcCall{
			clusterUUID: clusterUUID,
			recTypes:    append([]string(nil), recTypes...),
		})
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
	<-done

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(calls) == 3
	}, 2*time.Second, 10*time.Millisecond,
		"an arrival during a trailing run must be drained by the next loop pass, not lost to a snapshot-and-clear shortcut")

	mu.Lock()
	defer mu.Unlock()
	last := calls[len(calls)-1]
	assert.Equal(t, "cluster-D", last.clusterUUID,
		"cluster D (arrived during B's trailing run) should be the final recalculated cluster")
	assert.Equal(t, []string{"node"}, last.recTypes,
		"cluster D should be recalculated with its own recTypes")
}

// TestTriggerSavingsRecalcCoalesced_RearrivalDuringOwnRunReruns pins the
// trickiest interleaving: popPending removes an entry BEFORE its recalc runs,
// so a same-cluster arrival during that run must become a FRESH pending entry
// and trigger a second run (it is a new trigger and may carry new recTypes) —
// not merge into the already-consumed entry and vanish.
func TestTriggerSavingsRecalcCoalesced_RearrivalDuringOwnRunReruns(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "true")
	resetSavingsRecalcFlightsForTest()

	pool := testutil.SetupTestDB(t)
	orgID := "org-savings-recalc-rearrival"
	ctx := context.Background()

	var mu sync.Mutex
	var calls []savingsRecalcCall
	started := make(chan struct{})
	var once sync.Once
	var runCount atomic.Int32

	SetSavingsRecalcRunHookForTest(func(oid string, recTypes []string) {
		switch runCount.Add(1) {
		case 1:
			once.Do(func() { close(started) })
			time.Sleep(150 * time.Millisecond)
		case 2:
			// B's own trailing run is executing; its first entry was already
			// popped. Re-arrive as B with different recTypes — from inside
			// the run (the flight mutex is not held during a recalc).
			triggerSavingsRecalcCoalesced(ctx, pool, orgID, "cluster-B", []string{"pvc"})
			time.Sleep(150 * time.Millisecond)
		}
	})
	defer ClearSavingsRecalcRunHookForTest()

	restore := SetClusterSavingsRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID string, recTypes []string) error {
		mu.Lock()
		calls = append(calls, savingsRecalcCall{
			clusterUUID: clusterUUID,
			recTypes:    append([]string(nil), recTypes...),
		})
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
	<-done

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(calls) == 3
	}, 2*time.Second, 10*time.Millisecond,
		"A once plus B twice — a same-cluster re-arrival during its own run must become a fresh pending entry and run again")

	mu.Lock()
	defer mu.Unlock()
	var bCalls []savingsRecalcCall
	for _, c := range calls {
		if c.clusterUUID == "cluster-B" {
			bCalls = append(bCalls, c)
		}
	}
	require.Len(t, bCalls, 2, "cluster B should run twice: once per arrival")
	assert.Equal(t, []string{"container"}, bCalls[0].recTypes, "B's first run carries the first arrival's recTypes")
	assert.Equal(t, []string{"pvc"}, bCalls[1].recTypes, "B's second run carries the re-arrival's recTypes")
}

// TestTriggerSavingsRecalcCoalesced_OrgWideDoesNotSubsumeSpecifics pins the
// no-subsumption design (#812, owner decision): an org-wide ("") arrival and a
// specific-cluster arrival during a flight are separate pending entries —
// BOTH drain. The redundant specific recalc after an org-wide pass is a
// harmless no-op; subsumption would risk dropping a request whose recTypes
// the org-wide entry does not cover. On the pre-#812 single-slot guard the
// org-wide arrival is the one C's arrival overwrites — this test is RED there.
func TestTriggerSavingsRecalcCoalesced_OrgWideDoesNotSubsumeSpecifics(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "true")
	resetSavingsRecalcFlightsForTest()

	pool := testutil.SetupTestDB(t)
	orgID := "org-savings-recalc-nosub"
	ctx := context.Background()

	var mu sync.Mutex
	var calls []savingsRecalcCall
	started := make(chan struct{})
	var once sync.Once
	var runCount atomic.Int32

	SetSavingsRecalcRunHookForTest(func(oid string, recTypes []string) {
		if runCount.Add(1) == 1 {
			once.Do(func() { close(started) })
		}
		time.Sleep(150 * time.Millisecond)
	})
	defer ClearSavingsRecalcRunHookForTest()

	restore := SetClusterSavingsRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID string, recTypes []string) error {
		mu.Lock()
		calls = append(calls, savingsRecalcCall{
			clusterUUID: clusterUUID,
			recTypes:    append([]string(nil), recTypes...),
		})
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
	triggerSavingsRecalcCoalesced(ctx, pool, orgID, "", []string{"container"})
	triggerSavingsRecalcCoalesced(ctx, pool, orgID, "cluster-C", []string{"node"})
	<-done

	// Three RecalculateSavingsForOrg runs: A, the org-wide pass, and C. The
	// org-wide pass makes no per-cluster calls here (the test org has no
	// clusters in the DB), so the cluster-func captures pin A and C.
	require.EqualValues(t, 3, runCount.Load(),
		"the org-wide pass and the specific-cluster pass must BOTH run — no subsumption")

	mu.Lock()
	defer mu.Unlock()
	byCluster := map[string][]string{}
	for _, c := range calls {
		byCluster[c.clusterUUID] = c.recTypes
	}
	assert.Equal(t, []string{"container"}, byCluster["cluster-A"])
	assert.Equal(t, []string{"node"}, byCluster["cluster-C"],
		"the specific cluster C must not be dropped because an org-wide request is also pending")
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
