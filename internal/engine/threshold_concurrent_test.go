package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupThresholdConcurrentTest(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)
	return testutil.SetupTestDB(t), context.Background()
}

func TestThresholdCache_ConcurrentPUTs_DifferentOrgs(t *testing.T) {
	pool, ctx := setupThresholdConcurrentTest(t)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		orgID := fmt.Sprintf("org-threshold-concurrent-put-%d", i)
		wantCPU := 0.50 + float64(i)*0.01
		go func(oid string, cpu float64) {
			defer wg.Done()
			body := fmt.Sprintf(`{"cpu_cost_percentile": %.2f}`, cpu)
			if err := UpdateThresholdSettings(ctx, pool, oid, "container", json.RawMessage(body)); err != nil {
				errCh <- err
				return
			}
			got, err := ResolveContainerSizingThresholds(ctx, pool, oid)
			if err != nil {
				errCh <- err
				return
			}
			if got.CPUCostPercentile < cpu-1e-9 || got.CPUCostPercentile > cpu+1e-9 {
				errCh <- fmt.Errorf("org %s: got cpu_cost_percentile %.4f, want %.4f", oid, got.CPUCostPercentile, cpu)
			}
		}(orgID, wantCPU)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err, "concurrent PUT for different orgs should not corrupt threshold data")
	}
}

func TestThresholdCache_ConcurrentReadWrite_SameOrg(t *testing.T) {
	pool, ctx := setupThresholdConcurrentTest(t)
	orgID := "org-threshold-concurrent-rw"

	const readers = 5
	const writers = 2
	var wg sync.WaitGroup
	errCh := make(chan error, readers+writers)

	writeValues := []float64{0.55, 0.62}

	for w := 0; w < writers; w++ {
		wg.Add(1)
		val := writeValues[w]
		go func(v float64) {
			defer wg.Done()
			body := fmt.Sprintf(`{"cpu_cost_percentile": %.2f}`, v)
			if err := UpdateThresholdSettings(ctx, pool, orgID, "container", json.RawMessage(body)); err != nil {
				errCh <- err
			}
		}(val)
	}

	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				got, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
				if err != nil {
					errCh <- err
					return
				}
				if got.CPUCostPercentile < 0.01 || got.CPUCostPercentile > 1.0 {
					errCh <- fmt.Errorf("reader saw corrupt cpu_cost_percentile %.4f", got.CPUCostPercentile)
					return
				}
				if got.MinMargin <= 0 || got.MaxMargin <= 0 || got.MinMargin > got.MaxMargin {
					errCh <- fmt.Errorf("reader saw incomplete threshold set: min_margin=%.2f max_margin=%.2f", got.MinMargin, got.MaxMargin)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err, "concurrent readers should always observe a complete, valid threshold set")
	}
}

func TestThresholdRecalculation_ConcurrentTriggers_SameOrg(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_THRESHOLD_RECALCULATION_ENABLED", "true")

	pool := testutil.SetupTestDB(t)
	orgID := "org-threshold-concurrent-recalc"
	ctx := context.Background()

	var active int32
	var maxActive int32
	var completed int32
	done := make(chan struct{})

	restore := SetClusterRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, oid, clusterUUID, recType string) error {
		cur := atomic.AddInt32(&active, 1)
		for {
			prev := atomic.LoadInt32(&maxActive)
			if cur <= prev || atomic.CompareAndSwapInt32(&maxActive, prev, cur) {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		atomic.AddInt32(&completed, 1)
		return nil
	})
	defer restore()

	seedClustersForRecalcTest(t, pool, orgID, testutil.TestClusterUUID)

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			RecalculateThresholdsForOrg(ctx, pool, orgID, "container")
		}()
	}

	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent threshold recalculations deadlocked")
	}
	close(done)

	assert.GreaterOrEqual(t, atomic.LoadInt32(&completed), int32(1),
		"at least one recalculation should complete when two triggers race for the same org")
	assert.LessOrEqual(t, atomic.LoadInt32(&maxActive), int32(thresholdRecalcMaxConcurrent),
		"concurrent cluster recalcs should respect the semaphore limit")
}

func TestThresholdResolution_UnderLoad_NoRace(t *testing.T) {
	pool, ctx := setupThresholdConcurrentTest(t)

	const (
		goroutines = 50
		orgCount   = 20
	)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	errCh := make(chan error, goroutines)
	start := time.Now()

	for i := 0; i < goroutines; i++ {
		orgID := fmt.Sprintf("org-threshold-load-%d", i%orgCount)
		recType := []string{"container", "node", "gpu", "pvc"}[i%4]
		go func(oid, rt string) {
			defer wg.Done()
			var err error
			switch rt {
			case "container":
				_, err = ResolveContainerSizingThresholds(ctx, pool, oid)
			case "node":
				_, err = ResolveNodeThresholdSettings(ctx, pool, oid)
			case "gpu":
				_, err = ResolveGPUThresholdSettings(ctx, pool, oid)
			case "pvc":
				_, err = ResolvePVCThresholdSettings(ctx, pool, oid)
			}
			if err != nil {
				errCh <- err
			}
		}(orgID, recType)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err, "threshold resolution under load should not fail")
	}

	elapsed := time.Since(start)
	assert.Less(t, elapsed, 2*time.Second,
		"50 concurrent threshold resolutions across 20 orgs should complete within 2 seconds")
}
