package services

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
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func TestScheduleManifestRecommendations_DefersSynthesizedManifest(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SYNTH_MANIFEST_QUIET_PERIOD", "1")
	resetSynthManifestDebouncersForTest()
	t.Cleanup(resetSynthManifestDebouncersForTest)

	pool := testutil.SetupTestDB(t)
	msg := testKafkaMsg()
	msg.Object_keys = []string{"org1234567/source=abc/date=2026-06-11/ocp_ros_usage.csv"}

	var runCount atomic.Int32
	restore := setRunManifestRecommendationsHookForTest(func(context.Context, *pgxpool.Pool, types.KafkaMsg) error {
		runCount.Add(1)
		return nil
	})
	t.Cleanup(restore)

	before := promtest.ToFloat64(manifestRecommendationDeferredTotal)
	err := scheduleManifestRecommendations(context.Background(), pool, msg)
	require.NoError(t, err)
	assert.Equal(t, int32(0), runCount.Load())
	assert.InDelta(t, 1, promtest.ToFloat64(manifestRecommendationDeferredTotal)-before, 0)

	require.Eventually(t, func() bool {
		return runCount.Load() == 1
	}, 3*time.Second, 20*time.Millisecond)
}

func TestScheduleManifestRecommendations_RunsImmediatelyForRealManifest(t *testing.T) {
	config.ResetForTest()
	resetSynthManifestDebouncersForTest()

	pool := testutil.SetupTestDB(t)
	msg := testKafkaMsg()
	msg.Metadata.Manifest_id = "real-manifest-id"

	var runCount atomic.Int32
	restore := setRunManifestRecommendationsHookForTest(func(context.Context, *pgxpool.Pool, types.KafkaMsg) error {
		runCount.Add(1)
		return nil
	})
	t.Cleanup(restore)

	err := scheduleManifestRecommendations(context.Background(), pool, msg)
	require.NoError(t, err)
	assert.Equal(t, int32(1), runCount.Load())
}

func TestNotifySynthManifestFileActivity_ResetsQuietPeriod(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SYNTH_MANIFEST_QUIET_PERIOD", "1")
	resetSynthManifestDebouncersForTest()
	t.Cleanup(resetSynthManifestDebouncersForTest)

	pool := testutil.SetupTestDB(t)
	msg := testKafkaMsg()
	msg.Object_keys = []string{"org1234567/source=abc/date=2026-06-11/ocp_ros_usage.csv"}
	manifestID := synthesizeManifestID(msg)

	var runCount atomic.Int32
	var mu sync.Mutex
	restore := setRunManifestRecommendationsHookForTest(func(context.Context, *pgxpool.Pool, types.KafkaMsg) error {
		runCount.Add(1)
		return nil
	})
	t.Cleanup(restore)

	deferSynthManifestRecommendations(pool, msg, manifestID)
	time.Sleep(700 * time.Millisecond)
	notifySynthManifestFileActivity(manifestID)
	time.Sleep(700 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, int32(0), runCount.Load(), "quiet period reset should prevent early run")

	require.Eventually(t, func() bool {
		return runCount.Load() == 1
	}, 3*time.Second, 20*time.Millisecond)
}

func setRunManifestRecommendationsHookForTest(
	hook func(context.Context, *pgxpool.Pool, types.KafkaMsg) error,
) func() {
	prev := runManifestRecommendationsHook
	runManifestRecommendationsHook = hook
	return func() { runManifestRecommendationsHook = prev }
}

func TestShutdownSynthManifestDebouncers_SkipsPendingTimers(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SYNTH_MANIFEST_QUIET_PERIOD", "2")
	resetSynthManifestDebouncersForTest()
	t.Cleanup(resetSynthManifestDebouncersForTest)

	parent, cancel := context.WithCancel(context.Background())
	InitSynthManifestDebouncer(parent)
	t.Cleanup(cancel)

	pool := testutil.SetupTestDB(t)
	msg := testKafkaMsg()
	msg.Object_keys = []string{"org1234567/source=abc/date=2026-06-11/ocp_ros_usage.csv"}
	manifestID := synthesizeManifestID(msg)

	var runCount atomic.Int32
	restore := setRunManifestRecommendationsHookForTest(func(context.Context, *pgxpool.Pool, types.KafkaMsg) error {
		runCount.Add(1)
		return nil
	})
	t.Cleanup(restore)

	deferSynthManifestRecommendations(pool, msg, manifestID)
	ShutdownSynthManifestDebouncers()

	time.Sleep(2500 * time.Millisecond)
	assert.Equal(t, int32(0), runCount.Load(), "shutdown should prevent deferred recommendation runs")
}

func TestNotifySynthManifestFileActivity_NoDoubleFireUnderBurstyResets(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SYNTH_MANIFEST_QUIET_PERIOD", "1")
	resetSynthManifestDebouncersForTest()
	t.Cleanup(resetSynthManifestDebouncersForTest)

	pool := testutil.SetupTestDB(t)
	msg := testKafkaMsg()
	msg.Object_keys = []string{"org1234567/source=abc/date=2026-06-11/ocp_ros_usage.csv"}
	manifestID := synthesizeManifestID(msg)

	var runCount atomic.Int32
	fired := make(chan struct{})
	var firedOnce sync.Once
	restore := setRunManifestRecommendationsHookForTest(func(context.Context, *pgxpool.Pool, types.KafkaMsg) error {
		runCount.Add(1)
		firedOnce.Do(func() { close(fired) })
		return nil
	})
	t.Cleanup(restore)

	deferSynthManifestRecommendations(pool, msg, manifestID)
	for i := 0; i < 240; i++ {
		notifySynthManifestFileActivity(manifestID)
	}

	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for debounced manifest recommendations to fire")
	}
	assert.Equal(t, int32(1), runCount.Load(), "generation guard should allow only one deferred run")
}
