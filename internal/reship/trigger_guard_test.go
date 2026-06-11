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
}

func (c *countingTriggerer) TriggerReship(_ context.Context, _ string, clusterUUID uuid.UUID) error {
	if c.started != nil {
		select {
		case c.started <- struct{}{}:
		default:
		}
	}
	if c.delay > 0 {
		time.Sleep(c.delay)
	}
	c.mu.Lock()
	c.calls = append(c.calls, clusterUUID)
	c.mu.Unlock()
	return nil
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

	before := promtest.ToFloat64(reshipCoalescedTotal.WithLabelValues(orgID))

	TriggerAsync(trigger, orgID, []uuid.UUID{cluster})
	<-started
	TriggerAsync(trigger, orgID, []uuid.UUID{cluster})
	TriggerAsync(trigger, orgID, []uuid.UUID{cluster})

	require.Eventually(t, func() bool {
		after := promtest.ToFloat64(reshipCoalescedTotal.WithLabelValues(orgID))
		return after-before >= 2
	}, 2*time.Second, 10*time.Millisecond)

	assert.InDelta(t, 2, promtest.ToFloat64(reshipCoalescedTotal.WithLabelValues(orgID))-before, 0)
}

func TestTriggerReshipCoalesced_UsesLatestParameters(t *testing.T) {
	resetReshipFlightsForTest()

	orgID := "org-reship-latest"
	clusterA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	clusterB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	clusterC := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")

	var mu sync.Mutex
	var batches [][]uuid.UUID
	reshipBatchHook = func(ids []uuid.UUID) {
		mu.Lock()
		batches = append(batches, ids)
		mu.Unlock()
	}
	defer func() { reshipBatchHook = nil }()

	trigger := &countingTriggerer{delay: 150 * time.Millisecond, started: make(chan struct{}, 1)}
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		triggerReshipCoalesced(ctx, trigger, orgID, []uuid.UUID{clusterA})
		close(done)
	}()
	<-trigger.started
	triggerReshipCoalesced(ctx, trigger, orgID, []uuid.UUID{clusterB})
	triggerReshipCoalesced(ctx, trigger, orgID, []uuid.UUID{clusterC})
	<-done

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		if len(batches) < 2 {
			return false
		}
		last := batches[len(batches)-1]
		return len(last) == 1 && last[0] == clusterC
	}, 2*time.Second, 10*time.Millisecond)
}
