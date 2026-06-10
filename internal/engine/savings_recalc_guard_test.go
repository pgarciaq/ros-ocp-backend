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
