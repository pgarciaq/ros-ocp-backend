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
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestTriggerThresholdRecalculationAsync_CoalescesOverlappingJobs(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_THRESHOLD_RECALCULATION_ENABLED", "true")
	resetThresholdRecalcFlightsForTest()

	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-recalc-coalesce"

	var runCount atomic.Int32
	firstRunStarted := make(chan struct{})
	SetThresholdRecalcRunHookForTest(func(oid, rt string) {
		if runCount.Add(1) == 1 {
			close(firstRunStarted)
		}
		time.Sleep(100 * time.Millisecond)
	})
	defer ClearThresholdRecalcRunHookForTest()

	restore := SetClusterRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID, recType string) error {
		return nil
	})
	defer restore()

	for i := 0; i < 5; i++ {
		TriggerThresholdRecalculationAsync(pool, orgID, "container")
	}

	<-firstRunStarted
	require.Eventually(t, func() bool {
		return runCount.Load() >= 2
	}, 2*time.Second, 10*time.Millisecond, "expected initial run plus one coalesced follow-up")

	assert.Equal(t, int32(2), runCount.Load(), "rapid triggers should coalesce into at most two runs")
}

func TestTriggerThresholdRecalculationAsync_CoalescedMetricIncrements(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_THRESHOLD_RECALCULATION_ENABLED", "true")
	resetThresholdRecalcFlightsForTest()

	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-recalc-metric"

	started := make(chan struct{})
	var once sync.Once
	SetThresholdRecalcRunHookForTest(func(oid, rt string) {
		once.Do(func() { close(started) })
		time.Sleep(150 * time.Millisecond)
	})
	defer ClearThresholdRecalcRunHookForTest()

	restore := SetClusterRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID, recType string) error {
		return nil
	})
	defer restore()

	before := promtest.ToFloat64(thresholdRecalcCoalescedTotal.WithLabelValues(orgID, "gpu"))

	TriggerThresholdRecalculationAsync(pool, orgID, "gpu")
	<-started
	TriggerThresholdRecalculationAsync(pool, orgID, "gpu")
	TriggerThresholdRecalculationAsync(pool, orgID, "gpu")

	require.Eventually(t, func() bool {
		after := promtest.ToFloat64(thresholdRecalcCoalescedTotal.WithLabelValues(orgID, "gpu"))
		return after-before >= 2
	}, 2*time.Second, 10*time.Millisecond)

	assert.InDelta(t, 2, promtest.ToFloat64(thresholdRecalcCoalescedTotal.WithLabelValues(orgID, "gpu"))-before, 0)
}
