package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func seedClustersForRecalcTest(t *testing.T, pool *pgxpool.Pool, orgID string, clusterUUIDs ...string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (9001, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	for i, cu := range clusterUUIDs {
		_, err = pool.Exec(ctx, `
			INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
			VALUES (9001, $1::uuid, $2, $3, NOW())
			ON CONFLICT DO NOTHING`,
			cu, fmt.Sprintf("cluster-%d", i), fmt.Sprintf("src-%d", i))
		require.NoError(t, err)
	}
}

func TestListClustersForOrg(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-recalc-list"
	seedClustersForRecalcTest(t, pool, orgID,
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
	)

	clusters, err := ListClustersForOrg(context.Background(), pool, orgID)
	require.NoError(t, err)
	assert.Len(t, clusters, 2)
}

func TestRecalculateThresholdsForOrg_StopsOnContextCancel(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-recalc-cancel"
	seedClustersForRecalcTest(t, pool, orgID,
		"10101010-1010-1010-1010-101010101010",
		"20202020-2020-2020-2020-202020202020",
		"30303030-3030-3030-3030-303030303030",
	)

	block := make(chan struct{})
	restore := SetClusterRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID, recType string) error {
		<-block
		return nil
	})
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RecalculateThresholdsForOrg(ctx, pool, orgID, "container")
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()
	close(block)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RecalculateThresholdsForOrg did not exit after context cancellation")
	}
}

func TestRecalculateThresholdsForOrg_InvokesAllClusters(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-recalc-fanout"
	seedClustersForRecalcTest(t, pool, orgID,
		"cccccccc-cccc-cccc-cccc-cccccccccccc",
		"dddddddd-dddd-dddd-dddd-dddddddddddd",
	)

	var mu sync.Mutex
	var calls []struct {
		orgID, clusterUUID, recType string
	}
	restore := SetClusterRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID, recType string) error {
		mu.Lock()
		calls = append(calls, struct{ orgID, clusterUUID, recType string }{oid, clusterUUID, recType})
		mu.Unlock()
		return nil
	})
	defer restore()

	RecalculateThresholdsForOrg(context.Background(), pool, orgID, "container")

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, calls, 2)
	for _, c := range calls {
		assert.Equal(t, orgID, c.orgID)
		assert.Equal(t, "container", c.recType)
	}
}

func TestRecalculateThresholdsForOrg_ContinuesOnClusterError(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-recalc-partial"
	seedClustersForRecalcTest(t, pool, orgID,
		"eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee",
		"ffffffff-ffff-ffff-ffff-ffffffffffff",
	)

	var mu sync.Mutex
	processed := 0
	restore := SetClusterRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID, recType string) error {
		mu.Lock()
		defer mu.Unlock()
		if clusterUUID == "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee" {
			return fmt.Errorf("simulated failure")
		}
		processed++
		return nil
	})
	defer restore()

	RecalculateThresholdsForOrg(context.Background(), pool, orgID, "node")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, processed)
}

func TestTriggerThresholdRecalculationAsync_RespectsKillSwitch(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_THRESHOLD_RECALCULATION_ENABLED", "false")

	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-recalc-killswitch"
	triggered := false
	SetThresholdRecalcHookForTest(func(oid, rt string) {
		triggered = true
	})
	defer ClearThresholdRecalcHookForTest()

	TriggerThresholdRecalculationAsync(pool, orgID, "container")
	time.Sleep(50 * time.Millisecond)
	assert.False(t, triggered)
}

func TestTriggerThresholdRecalculationAsync_FiresHook(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_THRESHOLD_RECALCULATION_ENABLED", "true")

	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-recalc-hook"

	var mu sync.Mutex
	var hookedOrg, hookedType string
	SetThresholdRecalcHookForTest(func(oid, rt string) {
		mu.Lock()
		hookedOrg = oid
		hookedType = rt
		mu.Unlock()
	})
	defer ClearThresholdRecalcHookForTest()

	restore := SetClusterRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID, recType string) error {
		return nil
	})
	defer restore()

	TriggerThresholdRecalculationAsync(pool, orgID, "pvc")

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hookedOrg == orgID && hookedType == "pvc"
	}, time.Second, 10*time.Millisecond)
}

func TestTriggerThresholdRecalculationAsync_InvalidatesFleetSummaryCache(t *testing.T) {
	config.ResetForTest()
	fleetsummary.ResetForTest()
	t.Setenv("ROS_THRESHOLD_RECALCULATION_ENABLED", "true")
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_TTL", "3600")

	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-recalc-cache"

	fleetsummary.Put(orgID, false, nil, fleetsummary.CachedSummary{
		TotalContainers: 42,
		Currency:        "USD",
	})

	restore := SetClusterRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID, recType string) error {
		return nil
	})
	defer restore()

	TriggerThresholdRecalculationAsync(pool, orgID, "container")

	_, ok := fleetsummary.Get(orgID, false, nil)
	assert.False(t, ok, "threshold settings recalc trigger should invalidate fleet summary cache immediately (pre-recalc)")
}

func TestRecalculateThresholdsForOrg_PassesDateRangeToContainerRecalc(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_MAX_LOOKBACK_DAYS", "14")

	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-recalc-daterange"
	seedClustersForRecalcTest(t, pool, orgID, testutil.TestClusterUUID)

	before := time.Now().UTC()
	var capturedStart, capturedEnd time.Time
	restore := SetClusterRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID, recType string) error {
		if recType != "container" {
			return fmt.Errorf("unexpected type %q", recType)
		}
		start, end := recalcDateRange()
		capturedStart = start
		capturedEnd = end
		return nil
	})
	defer restore()

	RecalculateThresholdsForOrg(context.Background(), pool, orgID, "container")

	after := time.Now().UTC()
	expectedStart := before.AddDate(0, 0, -14)
	assert.WithinDuration(t, expectedStart, capturedStart, 2*time.Second)
	assert.WithinDuration(t, before, capturedEnd, 2*time.Second)
	assert.WithinDuration(t, after, capturedEnd, 2*time.Second)
}
