package reship

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// recordHook captures log records emitted through a fresh logger so tests can
// assert log cadence deterministically (no ticker, no sleeps).
type recordHook struct {
	mu      sync.Mutex
	records []*logrus.Entry
}

func (h *recordHook) Levels() []logrus.Level { return logrus.AllLevels }
func (h *recordHook) Fire(e *logrus.Entry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, e)
	return nil
}

// count matches a level and substring against the record's message and its
// structured "msg" field (the exhaustion log carries its tag in the field;
// the message is the raw error).
func (h *recordHook) count(level logrus.Level, substring string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level != level {
			continue
		}
		if strings.Contains(r.Message, substring) ||
			strings.Contains(fmt.Sprint(r.Data["msg"]), substring) {
			n++
		}
	}
	return n
}

// swapReshipLogForTest replaces the package logger with a capturing one and
// restores it on cleanup. Package tests run sequentially, so the swap is safe.
func swapReshipLogForTest(t *testing.T) *recordHook {
	t.Helper()
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	hook := &recordHook{}
	logger.AddHook(hook)
	prev := reshipLog
	reshipLog = logger.WithField("component", "reship")
	t.Cleanup(func() { reshipLog = prev })
	return hook
}

// TestReshipPoller_ExhaustedStopsWarning is #936's regression: once the retry
// budget is exhausted, tick must stop warning on every poll interval. The
// cluster whose reship can never succeed (masu always 503) gets exactly
// maxRetries attempts, exactly maxRetries tick warnings (one per real failed
// attempt), exactly one exhaustion ERROR, and then silence — the pending flag
// stays set so an API restart re-arms the budget (owner decision D2).
func TestReshipPoller_ExhaustedStopsWarning(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-exhausted"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	hook := swapReshipLogForTest(t)

	var masuCalls int
	masu := testMasuServer(func(w http.ResponseWriter, r *http.Request) {
		masuCalls++
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer masu.Close()

	require.NoError(t, MarkReshipPending(context.Background(), pool, orgID, clusterID))

	const maxRetries = 2
	poller := NewPoller(pool, PollerConfig{
		MasuURL:    masu.URL,
		Interval:   time.Hour, // irrelevant: ticks are driven synchronously
		MaxRetries: maxRetries,
	})
	require.NotNil(t, poller)

	const ticks = 8
	for i := 0; i < ticks; i++ {
		poller.tick(context.Background())
	}

	assert.Equal(t, maxRetries, masuCalls,
		"the retry budget must bound real masu attempts")
	assert.Equal(t, maxRetries, hook.count(logrus.WarnLevel, "RetryPending failed"),
		"tick warnings must stop after exhaustion: one per real failed attempt, not one per tick (#936)")
	assert.Equal(t, 1, hook.count(logrus.ErrorLevel, "reship max retries exceeded"),
		"the exhaustion ERROR fires exactly once, at the final real attempt")

	// D2: the flag stays set (restart re-arms the budget).
	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.NotNil(t, pending,
		"exhaustion without forward-only fallback must leave the pending flag set (restart re-arms)")
}

// TestReshipPoller_ForwardOnlyFallbackWired is the #936 wiring-gap leg:
// ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK was only ever applied to the
// API's one-shot path, never to the poller — the only path with a retry loop.
// With the fallback enabled, poller exhaustion must transition the cluster to
// forward-only: pending flag cleared, forward-only timestamp recorded.
func TestReshipPoller_ForwardOnlyFallbackWired(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-fallback"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	hook := swapReshipLogForTest(t)

	masu := testMasuServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer masu.Close()

	require.NoError(t, MarkReshipPending(context.Background(), pool, orgID, clusterID))

	const maxRetries = 2
	poller := NewPoller(pool, PollerConfig{
		MasuURL:             masu.URL,
		Interval:            time.Hour,
		MaxRetries:          maxRetries,
		ForwardOnlyFallback: true,
	})
	require.NotNil(t, poller)

	for i := 0; i < maxRetries+2; i++ {
		poller.tick(context.Background())
	}

	assert.Equal(t, 1, hook.count(logrus.WarnLevel, "retries exhausted, transitioning to forward-only"),
		"exhaustion with the fallback enabled must transition the cluster to forward-only exactly once")

	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.Nil(t, pending,
		"forward-only fallback must clear the pending flag (terminal state)")

	forwardOnly, err := ReshipForwardOnlySince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.NotNil(t, forwardOnly,
		"forward-only fallback must record reship_forward_only_since")
}
