package reship

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// masuServerWithRates fakes masu with an injectable effective_rates status,
// so tests can drive the provider resolver's permanent (404) and transient
// (5xx) failure classes through the full reship path.
func masuServerWithRates(ratesStatus int, ratesCalls *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/effective_rates/") {
			if ratesCalls != nil {
				atomic.AddInt32(ratesCalls, 1)
			}
			w.WriteHeader(ratesStatus)
			_, _ = w.Write([]byte(`{"Error": "No provider found for cluster_id=test."}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"files_processed":0,"files_total":0}`))
	}))
}

// TestReshipPoller_NoProviderClusterIsTerminal is #937's regression: a
// cluster whose provider resolution fails permanently (koku effective_rates
// 404 — the org has no source for it, so there is nothing to reship and the
// attempt can never succeed) must be TERMINAL on the first attempt: the
// pending flag cleared, one skip warning, no retry budget burned. Marking
// time is unchanged (the org-level PUT still marks every cluster; the flag
// resolves itself on the first poll).
func TestReshipPoller_NoProviderClusterIsTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-noprovider"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	hook := swapReshipLogForTest(t)

	var ratesCalls int32
	masu := masuServerWithRates(http.StatusNotFound, &ratesCalls)
	defer masu.Close()

	require.NoError(t, MarkReshipPending(context.Background(), pool, orgID, clusterID))

	poller := NewPoller(pool, PollerConfig{
		MasuURL:    masu.URL,
		MaxRetries: 2,
	})
	require.NotNil(t, poller)

	// Two ticks: the first must terminate the cluster; the second must find
	// nothing pending and do nothing.
	poller.tick(context.Background())
	poller.tick(context.Background())

	assert.EqualValues(t, 1, atomic.LoadInt32(&ratesCalls),
		"a permanent (404) resolution failure must be terminal on the first attempt — no retry budget burned")
	assert.Equal(t, 1, hook.count(logrus.WarnLevel, "no koku provider"),
		"the terminal decision logs exactly one skip warning")

	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.Nil(t, pending,
		"the pending flag must be cleared: the reship can never succeed and there is nothing to reship (#937 D2)")
}

// TestReshipPoller_TransientRatesFailureKeepsPending pins the unchanged
// leg: a transient resolution failure (masu 5xx) keeps today's semantics —
// pending stays set for poller retry, the retry budget applies.
func TestReshipPoller_TransientRatesFailureKeepsPending(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-transient"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	var ratesCalls int32
	masu := masuServerWithRates(http.StatusServiceUnavailable, &ratesCalls)
	defer masu.Close()

	require.NoError(t, MarkReshipPending(context.Background(), pool, orgID, clusterID))

	poller := NewPoller(pool, PollerConfig{
		MasuURL:    masu.URL,
		MaxRetries: 5,
	})
	require.NotNil(t, poller)

	poller.tick(context.Background())
	poller.tick(context.Background())

	assert.EqualValues(t, 2, atomic.LoadInt32(&ratesCalls),
		"transient failures keep the retry semantics — one real attempt per tick")
	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.NotNil(t, pending,
		"transient failures keep the pending flag set for poller retry (unchanged behavior)")
}
