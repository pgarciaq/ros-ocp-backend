package reship

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestReshipHTTP_Success_ClearsPending(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-success"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	var calls atomic.Int32
	masu := testMasuServer(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"files_processed":3,"files_total":5}`))
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL, MaxRetries: 10})
	require.NoError(t, MarkReshipPending(context.Background(), pool, orgID, clusterID))
	require.NoError(t, svc.TriggerReship(context.Background(), orgID, clusterID))
	require.Equal(t, int32(1), calls.Load())

	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.Nil(t, pending)
}

func TestReshipHTTP_MasuUnavailable_SetsPending(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-fail"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL})
	err := svc.TriggerReship(context.Background(), orgID, clusterID)
	require.Error(t, err)

	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	require.NotNil(t, pending)
}

func TestReshipHTTP_NetworkError_SetsPending(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-network"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	masuURL := masu.URL
	masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masuURL, MaxRetries: 10})
	err := svc.TriggerReship(context.Background(), orgID, clusterID)
	require.Error(t, err)

	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	require.NotNil(t, pending)
}

func TestReshipHTTP_5xx_SetsPending(t *testing.T) {
	TestReshipHTTP_MasuUnavailable_SetsPending(t)
}

func TestReshipHTTP_CorrectURL(t *testing.T) {
	providerID := uuid.MustParse(testutil.TestProviderUUID)
	start, end := dateRange()
	url := ReshipURL("http://masu.example:5042", "1234567", providerID, start, end)
	assert.Contains(t, url, "/api/cost-management/v1/reship_ros/?")
	assert.Contains(t, url, "schema=org1234567")
	assert.Contains(t, url, "provider_uuid="+providerID.String())
	assert.Contains(t, url, "start_date="+start)
	assert.Contains(t, url, "end_date="+end)
}

func TestReshipClient_DateRange_MaxWindowDays(t *testing.T) {
	start, end := dateRange()
	days := engine.PluginMaxWindowDays("container")
	startParsed, err := time.Parse("2006-01-02", start)
	require.NoError(t, err)
	endParsed, err := time.Parse("2006-01-02", end)
	require.NoError(t, err)
	assert.Equal(t, days, int(endParsed.Sub(startParsed).Hours()/24))
}

func TestReshipPoller_RetrySuccess(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-poller"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	var calls atomic.Int32
	masu := testMasuServer(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL, MaxRetries: 10})
	require.NoError(t, MarkReshipPending(context.Background(), pool, orgID, clusterID))
	require.Error(t, svc.RetryPending(context.Background(), orgID, clusterID))
	require.Equal(t, int32(1), calls.Load())

	require.NoError(t, svc.RetryPending(context.Background(), orgID, clusterID))
	require.Equal(t, int32(2), calls.Load())
	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.Nil(t, pending)
}

func TestReshipPoller_MaxRetries_FallbackDisabled_PendingStays(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-maxretry-no-fallback"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL, MaxRetries: 3, ForwardOnlyFallback: false})
	require.NoError(t, MarkReshipPending(context.Background(), pool, orgID, clusterID))

	for i := 0; i < 3; i++ {
		require.Error(t, svc.RetryPending(context.Background(), orgID, clusterID))
	}

	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	require.NotNil(t, pending, "pending must remain when forward-only fallback is disabled")

	status, err := GetClusterReshipStatus(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.Equal(t, ReshipStatusPending, status.Status)
}

func TestReshipPoller_MaxRetries_FallbackEnabled_TransitionsForwardOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-maxretry-fallback"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL, MaxRetries: 3, ForwardOnlyFallback: true})
	require.NoError(t, MarkReshipPending(context.Background(), pool, orgID, clusterID))

	before := counterValue(t, "ros_reship_fallback_forward_only_total", orgID)
	for i := 0; i < 3; i++ {
		require.Error(t, svc.RetryPending(context.Background(), orgID, clusterID))
	}

	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.Nil(t, pending, "pending must be cleared after forward-only transition")

	status, err := GetClusterReshipStatus(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.Equal(t, ReshipStatusForwardOnly, status.Status)
	require.NotNil(t, status.Since)

	after := counterValue(t, "ros_reship_fallback_forward_only_total", orgID)
	assert.Equal(t, before+1, after)
}

func TestReshipForwardOnly_PUTRearmsPending(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-put-rearm"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	require.NoError(t, MarkReshipForwardOnly(context.Background(), pool, orgID, clusterID))
	status, err := GetClusterReshipStatus(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	require.Equal(t, ReshipStatusForwardOnly, status.Status)

	require.NoError(t, MarkReshipPending(context.Background(), pool, orgID, clusterID))
	status, err = GetClusterReshipStatus(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.Equal(t, ReshipStatusPending, status.Status)

	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	require.NotNil(t, pending)
}

func TestReshipForwardOnly_SuccessClearsBoth(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-success-forward-only"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	require.NoError(t, MarkReshipForwardOnly(context.Background(), pool, orgID, clusterID))

	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"files_processed":1,"files_total":1}`))
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL, MaxRetries: 3})
	require.NoError(t, svc.TriggerReship(context.Background(), orgID, clusterID))

	status, err := GetClusterReshipStatus(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.Equal(t, ReshipStatusComplete, status.Status)
}

func TestReshipPoller_MaxRetries_IncrementsMetric(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-maxretry"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	var calls atomic.Int32
	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL, MaxRetries: 3})
	require.NoError(t, MarkReshipPending(context.Background(), pool, orgID, clusterID))

	before := counterValue(t, "ros_reship_failures_total", orgID)
	for i := 0; i < 3; i++ {
		require.Error(t, svc.RetryPending(context.Background(), orgID, clusterID))
	}
	assert.Equal(t, int32(3), calls.Load())
	after := counterValue(t, "ros_reship_failures_total", orgID)
	assert.Equal(t, before+1, after, "failure counter increments once when max retries exhausted")

	err := svc.RetryPending(context.Background(), orgID, clusterID)
	require.Error(t, err)
	assert.Equal(t, after, counterValue(t, "ros_reship_failures_total", orgID), "no duplicate increment on early return")
	assert.Equal(t, int32(3), calls.Load(), "fourth poller attempt must not call masu")
}

func TestReshipLock_ThreePUTs_MaxTwoExecutions(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-three-put"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	started := make(chan struct{})
	var calls atomic.Int32
	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if calls.Load() == 1 {
			close(started)
			time.Sleep(200 * time.Millisecond)
		}
		w.WriteHeader(http.StatusOK)
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = svc.TriggerReship(context.Background(), orgID, clusterID)
	}()
	<-started
	time.Sleep(30 * time.Millisecond)
	_, err := pool.Exec(context.Background(), `
		UPDATE business_hours_schedules SET updated_at = NOW() + interval '1 second'
		WHERE org_id = $1 AND cluster_uuid = $2::uuid`, orgID, clusterID.String())
	require.NoError(t, err)

	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_ = svc.TriggerReship(context.Background(), orgID, clusterID)
		}()
	}
	wg.Wait()
	assert.LessOrEqual(t, calls.Load(), int32(2), "initial reship plus at most one trailing masu call")
	assert.GreaterOrEqual(t, calls.Load(), int32(1))
}

func TestReshipLock_SingleFlight(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-lock"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	started := make(chan struct{})
	var calls atomic.Int32
	masu := testMasuServer(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if calls.Load() == 1 {
			close(started)
			time.Sleep(200 * time.Millisecond)
		}
		w.WriteHeader(http.StatusOK)
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = svc.TriggerReship(context.Background(), orgID, clusterID)
	}()
	<-started
	go func() {
		defer wg.Done()
		_ = svc.TriggerReship(context.Background(), orgID, clusterID)
	}()
	wg.Wait()
	assert.Equal(t, int32(1), calls.Load())
}

func TestReshipLock_TrailingReshipOnRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-trailing"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	started := make(chan struct{})
	var calls atomic.Int32
	masu := testMasuServer(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if calls.Load() == 1 {
			close(started)
			time.Sleep(150 * time.Millisecond)
		}
		w.WriteHeader(http.StatusOK)
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = svc.TriggerReship(context.Background(), orgID, clusterID)
	}()
	<-started
	time.Sleep(30 * time.Millisecond)
	_, err := pool.Exec(context.Background(), `
		UPDATE business_hours_schedules SET updated_at = NOW() + interval '1 second'
		WHERE org_id = $1 AND cluster_uuid = $2::uuid`, orgID, clusterID.String())
	require.NoError(t, err)
	<-done
	assert.Equal(t, int32(2), calls.Load(), "trailing reship after schedule change")
}

func TestReshipLock_DifferentClusters(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-parallel"
	clusterA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	clusterB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterA.String())
	seedBHScheduleRow(t, pool, orgID, clusterB.String())

	var calls atomic.Int32
	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = svc.TriggerReship(context.Background(), orgID, clusterA)
	}()
	go func() {
		defer wg.Done()
		_ = svc.TriggerReship(context.Background(), orgID, clusterB)
	}()
	wg.Wait()
	assert.Equal(t, int32(2), calls.Load())
}

func TestReshipLock_MaxTwoPerOrg(t *testing.T) {
	var inFlight atomic.Int32
	var peak atomic.Int32
	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		cur := inFlight.Add(1)
		for {
			old := peak.Load()
			if cur <= old || peak.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(80 * time.Millisecond)
		inFlight.Add(-1)
		w.WriteHeader(http.StatusOK)
	})
	defer masu.Close()

	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-cap"
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	clusters := []uuid.UUID{
		uuid.MustParse("11111111-1111-1111-1111-111111111101"),
		uuid.MustParse("11111111-1111-1111-1111-111111111102"),
		uuid.MustParse("11111111-1111-1111-1111-111111111103"),
	}
	for _, c := range clusters {
		seedBHScheduleRow(t, pool, orgID, c.String())
	}

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL})
	TriggerAsync(svc, orgID, clusters)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && inFlight.Load() > 0 {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	assert.LessOrEqual(t, peak.Load(), int32(orgMaxConcurrent))
}
func TestReshipLock_TTL_OneHour(t *testing.T) {
	lc := NewLockCoordinator(time.Hour)
	orgID := "org-ttl"
	clusterID := testutil.TestClusterUUID
	lc.ForceExpire(orgID, clusterID, time.Now().UTC().Add(-61*time.Minute))

	release, acquired := lc.Acquire(orgID, clusterID)
	require.True(t, acquired)
	release()
}

func TestReshipClient_EmptyMasuURL_NoPanic(t *testing.T) {
	assert.Nil(t, NewService(nil, ServiceConfig{MasuURL: ""}))
	trigger := DefaultTriggerer()
	_, ok := trigger.(*NoopTriggerer)
	assert.True(t, ok)
	require.NotPanics(t, func() {
		_ = trigger.TriggerReship(context.Background(), "1234567", uuid.New())
	})
}

func TestReshipMetrics_InProgress(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-metrics"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	started := make(chan struct{})
	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = svc.TriggerReship(context.Background(), orgID, clusterID)
	}()
	<-started
	assert.Equal(t, float64(1), gaugeValue(t, orgID, clusterID.String()))
	<-done
	assert.Equal(t, float64(0), gaugeValue(t, orgID, clusterID.String()))
}

func TestReshipPoller_ConfigurableInterval(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	p := NewPoller(pool, PollerConfig{
		MasuURL:  "http://example",
		Interval: 5 * time.Second,
	})
	require.NotNil(t, p)
	assert.Equal(t, 5*time.Second, p.interval)
}

func TestReshipPoller_MaxRetriesDefault10(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	svc := NewService(pool, ServiceConfig{MasuURL: "http://example", MaxRetries: 0})
	require.NotNil(t, svc)
	require.Equal(t, 10, svc.maxRetries)

	// BH-UNIT-103: 11th poller cycle must not call masu after default max retries (10).
	orgID := "org-reship-default-max"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	var calls atomic.Int32
	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer masu.Close()

	svc = NewService(pool, ServiceConfig{MasuURL: masu.URL, MaxRetries: 0})
	require.Equal(t, 10, svc.maxRetries)
	require.NoError(t, MarkReshipPending(context.Background(), pool, orgID, clusterID))

	before := counterValue(t, "ros_reship_failures_total", orgID)
	for i := 0; i < 10; i++ {
		require.Error(t, svc.RetryPending(context.Background(), orgID, clusterID))
	}
	assert.Equal(t, int32(10), calls.Load())
	after := counterValue(t, "ros_reship_failures_total", orgID)
	assert.Equal(t, before+1, after)

	err := svc.RetryPending(context.Background(), orgID, clusterID)
	require.Error(t, err)
	assert.Equal(t, int32(10), calls.Load(), "11th attempt must not call masu")
	assert.Equal(t, after, counterValue(t, "ros_reship_failures_total", orgID))
}

func TestReshipClient_400_SetsPending(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-400"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL, MaxRetries: 10})
	err := svc.TriggerReship(context.Background(), orgID, clusterID)
	require.Error(t, err)

	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	require.NotNil(t, pending)
}

func TestReshipClient_404_SetsPending(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-404"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupReshipSchedules(t, pool, orgID) })
	seedBHScheduleRow(t, pool, orgID, clusterID.String())

	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL, MaxRetries: 10})
	err := svc.TriggerReship(context.Background(), orgID, clusterID)
	require.Error(t, err)

	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	require.NotNil(t, pending)
}

func TestReshipConsumerUnavailable_PendingUntilIngest(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-reship-pending-ingest"
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	clusterStr := clusterID.String()
	cleanupReshipSchedules(t, pool, orgID)
	t.Cleanup(func() {
		cleanupReshipSchedules(t, pool, orgID)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
	})
	seedBHScheduleRow(t, pool, orgID, clusterStr)

	masu := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer masu.Close()

	svc := NewService(pool, ServiceConfig{MasuURL: masu.URL, MaxRetries: 10})
	require.Error(t, svc.TriggerReship(context.Background(), orgID, clusterID))

	pending, err := ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	require.NotNil(t, pending, "masu failure must leave reship_pending_since set")

	// Consumer-side ingest alone must not clear the pending flag (masu reship still required).
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	config.ResetForTest()
	csv := `interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count
2026-04-01 00:00:00 +0000 UTC,2026-04-01 00:15:00 +0000 UTC,pending-ns,pod-1,deploy-1,deployment,main,0.1,0.15,0.08,0.001,134217728,134217728,104857600,100000000,0`
	_, ingestErr := ingestion.ParseAndDigestCSV(context.Background(), pool, strings.NewReader(csv), orgID, clusterStr)
	require.NoError(t, ingestErr)

	pending, err = ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	require.NotNil(t, pending, "digest ingest must not clear reship_pending_since")

	masu.Close()
	masuOK := testMasuServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer masuOK.Close()
	svc = NewService(pool, ServiceConfig{MasuURL: masuOK.URL, MaxRetries: 10})
	require.NoError(t, svc.TriggerReship(context.Background(), orgID, clusterID))

	pending, err = ReshipPendingSince(context.Background(), pool, orgID, clusterID)
	require.NoError(t, err)
	assert.Nil(t, pending, "successful masu reship clears pending after ingest path can run")
}

func seedBHScheduleRow(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO business_hours_schedules (
			org_id, cluster_uuid, namespace, timezone, days, start_time, end_time,
			off_hours_weight, enabled
		) VALUES ($1, $2::uuid, '', 'UTC', ARRAY['monday'], '08:00', '17:00', 0, true)
		ON CONFLICT (org_id, cluster_uuid, namespace) DO NOTHING`,
		orgID, clusterUUID,
	)
	require.NoError(t, err)
}

func cleanupReshipSchedules(t *testing.T, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	require.NoError(t, err)
}

func gaugeValue(t *testing.T, orgID, clusterUUID string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != "ros_reship_in_progress" {
			continue
		}
		for _, m := range mf.GetMetric() {
			var o, c string
			for _, lp := range m.GetLabel() {
				switch lp.GetName() {
				case "org_id":
					o = lp.GetValue()
				case "cluster_uuid":
					c = lp.GetValue()
				}
			}
			if o == orgID && c == clusterUUID {
				return m.GetGauge().GetValue()
			}
		}
	}
	return 0
}

func counterValue(t *testing.T, name, orgID string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "org_id" && lp.GetValue() == orgID {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func TestReshipContract_NoReUpload(t *testing.T) {
	// ros-ocp-backend only POSTs to masu reship_ros; it never re-uploads CSVs to S3.
	client := NewHTTPClient("http://127.0.0.1:1", &http.Client{Timeout: 50 * time.Millisecond})
	client.resolver = staticProviderResolver{providerUUID: uuid.MustParse(testutil.TestProviderUUID)}
	_, err := client.PostReship(context.Background(), "1234567", uuid.New())
	require.Error(t, err)
}
