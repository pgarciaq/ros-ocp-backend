package reship

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	promtest "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingTriggerer struct {
	mu      sync.Mutex
	calls   []uuid.UUID
	delay   time.Duration
	started chan struct{}
	proceed chan struct{} // optional gate: block until signaled (nil = no gate)
	err     error         // optional: returned from every TriggerReship call
}

func (c *countingTriggerer) TriggerReship(_ context.Context, _ string, clusterUUID uuid.UUID) error {
	if c.started != nil {
		select {
		case c.started <- struct{}{}:
		default:
		}
	}
	if c.proceed != nil {
		<-c.proceed
	}
	if c.delay > 0 {
		time.Sleep(c.delay)
	}
	c.mu.Lock()
	c.calls = append(c.calls, clusterUUID)
	c.mu.Unlock()
	return c.err
}

func (c *countingTriggerer) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func TestTriggerAsync_CoalescesOverlappingJobs(t *testing.T) {
	resetReshipFlightsForTest()

	orgID := "org-reship-coalesce"
	cluster := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	started := make(chan struct{}, 1)
	trigger := &countingTriggerer{delay: 100 * time.Millisecond, started: started}

	for i := 0; i < 5; i++ {
		TriggerAsync(trigger, orgID, []uuid.UUID{cluster})
	}

	<-started

	require.Eventually(t, func() bool {
		return trigger.callCount() >= 2
	}, 2*time.Second, 10*time.Millisecond, "expected initial run plus one coalesced follow-up")

	assert.LessOrEqual(t, trigger.callCount(), 2, "rapid triggers should coalesce into at most two runs")
}

func TestTriggerAsync_CoalescedMetricIncrements(t *testing.T) {
	resetReshipFlightsForTest()

	orgID := "org-reship-metric"
	cluster := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	started := make(chan struct{}, 1)
	trigger := &countingTriggerer{delay: 150 * time.Millisecond, started: started}

	before := promtest.ToFloat64(reshipCoalescedTotal)

	TriggerAsync(trigger, orgID, []uuid.UUID{cluster})
	<-started
	TriggerAsync(trigger, orgID, []uuid.UUID{cluster})
	TriggerAsync(trigger, orgID, []uuid.UUID{cluster})

	require.Eventually(t, func() bool {
		after := promtest.ToFloat64(reshipCoalescedTotal)
		return after-before >= 2
	}, 2*time.Second, 10*time.Millisecond)

	assert.InDelta(t, 2, promtest.ToFloat64(reshipCoalescedTotal)-before, 0)
}

func TestTriggerReshipCoalesced_DrainsPendingSet(t *testing.T) {
	resetReshipFlightsForTest()

	orgID := "org-reship-drain"
	clusterA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	clusterB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	clusterC := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")

	var mu sync.Mutex
	var batches [][]uuid.UUID
	reshipBatchHookMu.Lock()
	reshipBatchHook = func(ids []uuid.UUID) {
		mu.Lock()
		batches = append(batches, ids)
		mu.Unlock()
	}
	reshipBatchHookMu.Unlock()
	defer func() {
		reshipBatchHookMu.Lock()
		reshipBatchHook = nil
		reshipBatchHookMu.Unlock()
	}()

	proceed := make(chan struct{})
	trigger := &countingTriggerer{started: make(chan struct{}, 1), proceed: proceed}
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		triggerReshipCoalesced(ctx, trigger, orgID, []uuid.UUID{clusterA})
		close(done)
	}()
	<-trigger.started
	// #863: two scoped edits coalescing during one running batch — both
	// sets must survive the trailing run (latest-wins dropped clusterB).
	triggerReshipCoalesced(ctx, trigger, orgID, []uuid.UUID{clusterB})
	triggerReshipCoalesced(ctx, trigger, orgID, []uuid.UUID{clusterC})
	close(proceed)
	<-done

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(batches), 2, "expected initial run plus one coalesced follow-up")
	last := batches[len(batches)-1]
	assert.ElementsMatch(t, []uuid.UUID{clusterB, clusterC}, last,
		"coalesced follow-up should drain the SET of pending clusters, not the latest slot (#863, #812 parity)")
}

func TestTriggerReshipCoalesced_MergesOrgThenClusterScoped(t *testing.T) {
	resetReshipFlightsForTest()

	orgID := "org-reship-org-then-cluster"
	clusterA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	clusterW := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	clusterX := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")

	var mu sync.Mutex
	var batches [][]uuid.UUID
	reshipBatchHookMu.Lock()
	reshipBatchHook = func(ids []uuid.UUID) {
		mu.Lock()
		batches = append(batches, ids)
		mu.Unlock()
	}
	reshipBatchHookMu.Unlock()
	defer func() {
		reshipBatchHookMu.Lock()
		reshipBatchHook = nil
		reshipBatchHookMu.Unlock()
	}()

	proceed := make(chan struct{})
	trigger := &countingTriggerer{started: make(chan struct{}, 1), proceed: proceed}
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		triggerReshipCoalesced(ctx, trigger, orgID, []uuid.UUID{clusterA})
		close(done)
	}()
	<-trigger.started
	// The discriminating ordering (#863): an org-level edit (a broad set)
	// coalesces FIRST, then a cluster-level edit arrives — latest-wins kept
	// only the narrow set, dropping the org edit's other clusters.
	triggerReshipCoalesced(ctx, trigger, orgID, []uuid.UUID{clusterW, clusterX})
	triggerReshipCoalesced(ctx, trigger, orgID, []uuid.UUID{clusterX})
	close(proceed)
	<-done

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(batches), 2, "expected initial run plus one coalesced follow-up")
	last := batches[len(batches)-1]
	assert.ElementsMatch(t, []uuid.UUID{clusterW, clusterX}, last,
		"the org-level set must survive a later cluster-scoped arrival — concurrent edits at different scopes must both reship (#863)")
	assert.Len(t, last, 2, "the union dedupes: cluster X appears once")
}

func TestTriggerReshipCoalesced_ConsumesSetAcrossFlights(t *testing.T) {
	resetReshipFlightsForTest()

	orgID := "org-reship-consume"
	clusterA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	clusterB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	var mu sync.Mutex
	var batches [][]uuid.UUID
	reshipBatchHookMu.Lock()
	reshipBatchHook = func(ids []uuid.UUID) {
		mu.Lock()
		batches = append(batches, ids)
		mu.Unlock()
	}
	reshipBatchHookMu.Unlock()
	defer func() {
		reshipBatchHookMu.Lock()
		reshipBatchHook = nil
		reshipBatchHookMu.Unlock()
	}()

	trigger := &countingTriggerer{}
	ctx := context.Background()

	// Flight 1 with clusterA completes fully...
	triggerReshipCoalesced(ctx, trigger, orgID, []uuid.UUID{clusterA})
	// ...then a new flight starts for clusterB: its batch must not merge
	// clusterA back in (the snapshot-and-clear concern — without the
	// consume-on-read, the union would re-reship the previous set).
	triggerReshipCoalesced(ctx, trigger, orgID, []uuid.UUID{clusterB})

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, batches, 2, "two sequential flights produce two batches")
	assert.Equal(t, []uuid.UUID{clusterA}, batches[0])
	assert.Equal(t, []uuid.UUID{clusterB}, batches[1],
		"a new flight's batch must not carry the previous flight's clusters (consume-on-read, #812's M3)")
}

// #534: per-cluster trigger failures must surface via rosocp_reship_errors_total.
// runReshipBatch is synchronous, so no Eventually polling is needed.
func TestRunReshipBatch_SurfacesTriggerErrors(t *testing.T) {
	resetReshipFlightsForTest()

	orgID := "org-reship-errors"
	c1 := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	c2 := uuid.MustParse("55555555-5555-5555-5555-555555555555")

	before := promtest.ToFloat64(reshipErrorsTotal)
	failing := &countingTriggerer{err: assert.AnError}
	runReshipBatch(context.Background(), failing, orgID, []uuid.UUID{c1, c2})
	assert.Equal(t, 2, failing.callCount(), "batch must attempt every cluster even when all fail")
	assert.InDelta(t, 2, promtest.ToFloat64(reshipErrorsTotal)-before, 0,
		"each failed cluster must increment rosocp_reship_errors_total")

	// Success path leaves the counter untouched.
	healthy := &countingTriggerer{}
	runReshipBatch(context.Background(), healthy, orgID, []uuid.UUID{c1})
	assert.Equal(t, 1, healthy.callCount())
	assert.InDelta(t, 2, promtest.ToFloat64(reshipErrorsTotal)-before, 0,
		"successful triggers must not increment the error counter")
}
