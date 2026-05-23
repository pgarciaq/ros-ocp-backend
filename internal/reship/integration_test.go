package reship

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestReshipPending_PollerClears(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-int-poller"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	var calls int
	masu := testMasuServer(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	defer masu.Close()

	require.NoError(t, MarkReshipPending(context.Background(), pool, orgID, clusterID))

	poller := NewPoller(pool, PollerConfig{
		MasuURL:  masu.URL,
		Interval: 50 * time.Millisecond,
		MaxRetries: 5,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go poller.Run(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
		require.NoError(t, err)
		if pending == nil && calls >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.Nil(t, pending)
	assert.GreaterOrEqual(t, calls, 2)
}

func TestReshipLock_Storage(t *testing.T) {
	lc := NewLockCoordinator(time.Hour)
	release1, ok1 := lc.Acquire("org-a", "cluster-1")
	require.True(t, ok1)
	_, ok2 := lc.Acquire("org-a", "cluster-1")
	require.False(t, ok2)
	release2, ok3 := lc.Acquire("org-a", "cluster-2")
	require.True(t, ok3)
	release1()
	release2()
	release3, ok4 := lc.Acquire("org-a", "cluster-1")
	require.True(t, ok4)
	release3()
}
