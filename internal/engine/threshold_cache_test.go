package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestResolveThresholdCached_ReturnsCachedValue(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-cache-hit"

	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'container', '{"cpu_cost_percentile": 0.72}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got1, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got1.CPUCostPercentile, 1e-9)

	_, err = pool.Exec(ctx, `
		UPDATE recommendation_thresholds
		SET thresholds = '{"cpu_cost_percentile": 0.55}'::jsonb
		WHERE org_id = $1 AND recommendation_type = 'container'`, orgID)
	require.NoError(t, err)

	got2, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got2.CPUCostPercentile, 1e-9, "second call should return cached value without re-reading DB")
}

func TestResolveThresholdCached_ExpiredTTL_RefetchesFromDB(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-cache-ttl"

	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := start
	restore := SetThresholdSettingsNowForTest(func() time.Time { return clock })
	defer restore()

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'container', '{"cpu_cost_percentile": 0.72}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got1, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got1.CPUCostPercentile, 1e-9)

	_, err = pool.Exec(ctx, `
		UPDATE recommendation_thresholds
		SET thresholds = '{"cpu_cost_percentile": 0.61}'::jsonb
		WHERE org_id = $1 AND recommendation_type = 'container'`, orgID)
	require.NoError(t, err)

	clock = start.Add(thresholdSettingsCacheTTL + time.Second)

	got2, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.61, got2.CPUCostPercentile, 1e-9, "expired cache should refetch from DB")
}

func TestInvalidateThresholdCache_ClearsOrgEntry(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-cache-invalidate"

	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'container', '{"cpu_cost_percentile": 0.72}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got1, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got1.CPUCostPercentile, 1e-9)

	_, err = pool.Exec(ctx, `
		UPDATE recommendation_thresholds
		SET thresholds = '{"cpu_cost_percentile": 0.63}'::jsonb
		WHERE org_id = $1 AND recommendation_type = 'container'`, orgID)
	require.NoError(t, err)

	gotCached, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, gotCached.CPUCostPercentile, 1e-9)

	InvalidateThresholdCache(orgID, "container")

	got2, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.63, got2.CPUCostPercentile, 1e-9)
}

func TestInvalidateThresholdCache_DoesNotAffectOtherOrgs(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgA := "org-threshold-cache-a"
	orgB := "org-threshold-cache-b"

	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	for _, org := range []string{orgA, orgB} {
		_, err := pool.Exec(ctx, `
			INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
			VALUES ($1, 'node', '{"cost_target_utilization": 0.72}'::jsonb)
			ON CONFLICT (org_id, recommendation_type)
			DO UPDATE SET thresholds = EXCLUDED.thresholds`, org)
		require.NoError(t, err)
	}

	gotA, err := ResolveNodeThresholdSettings(ctx, pool, orgA)
	require.NoError(t, err)
	gotB, err := ResolveNodeThresholdSettings(ctx, pool, orgB)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, gotA.CostTargetUtilization, 1e-9)
	assert.InDelta(t, 0.72, gotB.CostTargetUtilization, 1e-9)

	_, err = pool.Exec(ctx, `
		UPDATE recommendation_thresholds
		SET thresholds = '{"cost_target_utilization": 0.55}'::jsonb
		WHERE org_id = $1 AND recommendation_type = 'node'`, orgA)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE recommendation_thresholds
		SET thresholds = '{"cost_target_utilization": 0.66}'::jsonb
		WHERE org_id = $1 AND recommendation_type = 'node'`, orgB)
	require.NoError(t, err)

	InvalidateThresholdCache(orgA, "node")

	gotA2, err := ResolveNodeThresholdSettings(ctx, pool, orgA)
	require.NoError(t, err)
	gotB2, err := ResolveNodeThresholdSettings(ctx, pool, orgB)
	require.NoError(t, err)

	assert.InDelta(t, 0.55, gotA2.CostTargetUtilization, 1e-9)
	assert.InDelta(t, 0.72, gotB2.CostTargetUtilization, 1e-9, "org B cache should remain valid after invalidating org A")
}

func TestResolveThresholdCached_ConcurrentAccess(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-cache-concurrent"

	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			got, err := ResolvePVCThresholdSettings(ctx, pool, orgID)
			if err != nil {
				errCh <- err
				return
			}
			want := DefaultPVCThresholdSettings()
			if got.OversizedThreshold != want.OversizedThreshold ||
				got.NearFullThreshold != want.NearFullThreshold ||
				got.MinTrendDays != want.MinTrendDays {
				errCh <- assert.AnError
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}
