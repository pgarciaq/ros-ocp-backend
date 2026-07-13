package snapshot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestStorageGBUsageRateFromCostData(t *testing.T) {
	costData := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			storageGBUsagePerMonthMetric: {Infrastructure: 0.02, Supplementary: 0.01},
		},
	}
	rate, ok := storageGBUsageRateFromCostData(costData)
	assert.True(t, ok)
	assert.InDelta(t, 0.03, rate, 1e-9)

	_, ok = storageGBUsageRateFromCostData(nil)
	assert.False(t, ok)

	empty := &costdata.ClusterCostData{ConfiguredRates: map[string]costdata.RatePair{}}
	_, ok = storageGBUsageRateFromCostData(empty)
	assert.False(t, ok)

	zeroSum := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			storageGBUsagePerMonthMetric: {Infrastructure: 0, Supplementary: 0},
		},
	}
	_, ok = storageGBUsageRateFromCostData(zeroSum)
	assert.False(t, ok)
}

func TestResolveCostPerGiBMonth_EffectiveRates(t *testing.T) {
	config.ResetForTest()
	cfg := config.GetConfig()

	costData := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			storageGBUsagePerMonthMetric: {Infrastructure: 0.02, Supplementary: 0.01},
		},
	}

	got := resolveCostPerGiBMonth(cfg, false, 0, costData)
	assert.InDelta(t, 0.03, got, 1e-9)
}

func TestResolveCostPerGiBMonth_EnvOverridesEffectiveRates(t *testing.T) {
	t.Setenv("ROS_SNAPSHOT_COST_PER_GIB_MONTH", "0.08")
	config.ResetForTest()
	cfg := config.GetConfig()

	costData := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			storageGBUsagePerMonthMetric: {Infrastructure: 0.02, Supplementary: 0.01},
		},
	}

	got := resolveCostPerGiBMonth(cfg, false, 0, costData)
	assert.InDelta(t, 0.08, got, 1e-9)
}

func TestResolveCostPerGiBMonth_DBOverridesEffectiveRates(t *testing.T) {
	config.ResetForTest()
	cfg := config.GetConfig()

	costData := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			storageGBUsagePerMonthMetric: {Infrastructure: 0.02, Supplementary: 0.01},
		},
	}

	got := resolveCostPerGiBMonth(cfg, true, 0.12, costData)
	assert.InDelta(t, 0.12, got, 1e-9)
}

func TestResolveCostPerGiBMonth_DBOverridesEnv(t *testing.T) {
	t.Setenv("ROS_SNAPSHOT_COST_PER_GIB_MONTH", "0.08")
	config.ResetForTest()
	cfg := config.GetConfig()

	got := resolveCostPerGiBMonth(cfg, true, 0.12, nil)
	assert.InDelta(t, 0.12, got, 1e-9)
}

func TestResolveCostPerGiBMonth_FallbackCompiledDefault(t *testing.T) {
	config.ResetForTest()
	cfg := config.GetConfig()

	got := resolveCostPerGiBMonth(cfg, false, 0, nil)
	assert.InDelta(t, SnapshotSettingsDefaults.CostPerGiBMonth, got, 1e-9)
}

func TestResolveSnapshotSettings_EffectiveRatesIntegration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-snapshot-cost-dynamic"

	config.ResetForTest()

	costData := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			storageGBUsagePerMonthMetric: {Infrastructure: 0.02, Supplementary: 0.01},
		},
	}

	settings, err := ResolveSnapshotSettings(ctx, pool, orgID, costData)
	require.NoError(t, err)
	assert.InDelta(t, 0.03, settings.CostPerGiBMonth, 1e-9)
}

func TestResolveSnapshotSettings_DBOverrideIntegration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-snapshot-cost-db"

	config.ResetForTest()

	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_settings (
			org_id, orphan_age_days, never_restored_days, stale_days,
			redundant_threshold, cost_per_gib_month_usd, inventory_fresh_hours, updated_at
		) VALUES ($1, 7, 30, 90, 3, 0.15, 6, NOW())`, orgID)
	require.NoError(t, err)

	costData := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			storageGBUsagePerMonthMetric: {Infrastructure: 0.02, Supplementary: 0.01},
		},
	}

	settings, err := ResolveSnapshotSettings(ctx, pool, orgID, costData)
	require.NoError(t, err)
	assert.InDelta(t, 0.15, settings.CostPerGiBMonth, 1e-6)
}

func TestResolveSnapshotSettings_InventoryFreshHours_DBAndEnv(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-snapshot-fresh-hours"

	config.ResetForTest()

	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_settings (
			org_id, orphan_age_days, never_restored_days, stale_days,
			redundant_threshold, cost_per_gib_month_usd, inventory_fresh_hours, updated_at
		) VALUES ($1, 7, 30, 90, 3, 0.05, 12, NOW())`, orgID)
	require.NoError(t, err)

	settings, err := ResolveSnapshotSettings(ctx, pool, orgID, nil)
	require.NoError(t, err)
	require.Equal(t, 12, settings.InventoryFreshHours)

	t.Setenv("ROS_SNAPSHOT_INVENTORY_FRESH_HOURS", "24")
	config.ResetForTest()
	settings, err = ResolveSnapshotSettings(ctx, pool, orgID, nil)
	require.NoError(t, err)
	require.Equal(t, 24, settings.InventoryFreshHours)
}

func TestResolveSnapshotSettings_NilCostDataSkipsEffectiveRates(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-snapshot-cost-api"

	config.ResetForTest()

	settings, err := ResolveSnapshotSettings(ctx, pool, orgID, nil)
	require.NoError(t, err)
	assert.InDelta(t, SnapshotSettingsDefaults.CostPerGiBMonth, settings.CostPerGiBMonth, 1e-9)
}
