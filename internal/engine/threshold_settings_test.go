package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestResolveContainerSizingThresholds_DefaultsWithoutOverrides(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-defaults"

	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())

	got, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	want := DefaultContainerSizingThresholds()
	assert.Equal(t, want, got)
}

func TestResolveContainerSizingThresholds_DBOverride(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-db"

	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'container', '{"cpu_cost_percentile": 0.72}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got.CPUCostPercentile, 1e-9)
	assert.InDelta(t, DefaultContainerSizingThresholds().CPUPerfPercentile, got.CPUPerfPercentile, 1e-9)
}

func TestResolveContainerSizingThresholds_EnvOverridesDB(t *testing.T) {
	t.Setenv("ROS_CONTAINER_CPU_COST_PERCENTILE", "0.88")
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-env"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'container', '{"cpu_cost_percentile": 0.72}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.88, got.CPUCostPercentile, 1e-9)
}

func TestGetThresholdSettingsForAPI_LockedFieldsWhenEnvSet(t *testing.T) {
	t.Setenv("ROS_CONTAINER_MIN_MARGIN", "1.20")
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-locked"

	resp, err := GetThresholdSettingsForAPI(ctx, pool, orgID, "container")
	require.NoError(t, err)

	settings, ok := resp.(SizingThresholdSettingsResponse)
	require.True(t, ok)
	assert.Contains(t, settings.LockedFields, "min_margin")
	assert.InDelta(t, 1.20, settings.MinMargin, 1e-9)
}

func TestUpdateThresholdSettings_RejectsLockedField(t *testing.T) {
	t.Setenv("ROS_CONTAINER_CPU_COST_PERCENTILE", "0.70")
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-put-locked"

	err := UpdateThresholdSettings(ctx, pool, orgID, "container", json.RawMessage(`{"cpu_cost_percentile": 0.55}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFieldsLocked)
}

func TestUpdateThresholdSettings_PersistsAndDeleteResets(t *testing.T) {
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-crud"

	err := UpdateThresholdSettings(ctx, pool, orgID, "container", json.RawMessage(`{"cpu_cost_percentile": 0.71}`))
	require.NoError(t, err)

	got, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.71, got.CPUCostPercentile, 1e-9)

	err = DeleteThresholdSettings(ctx, pool, orgID, "container")
	require.NoError(t, err)

	got, err = ResolveContainerSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, DefaultContainerSizingThresholds(), got)
}

func TestCPUConfigFromSizing_UsesResolvedPercentiles(t *testing.T) {
	now := time.Now().UTC()
	th := DefaultContainerSizingThresholds()
	th.CPUCostPercentile = 0.75
	th.CPUPerfPercentile = 0.99

	costCfg := CPUConfigFromSizing(th, now, 168, "cost")
	assert.InDelta(t, 0.75, costCfg.CostPercentile, 1e-9)

	perfCfg := CPUConfigFromSizing(th, now, 168, "performance")
	assert.InDelta(t, 0.99, perfCfg.CostPercentile, 1e-9)
	assert.InDelta(t, 0.99, perfCfg.PerfPercentile, 1e-9)
}

func TestSnapshotInventoryFreshHours_FromConfig(t *testing.T) {
	config.ResetForTest()
	assert.Equal(t, 6, SnapshotInventoryFreshHours())

	t.Setenv("ROS_SNAPSHOT_INVENTORY_FRESH_HOURS", "12")
	config.ResetForTest()
	assert.Equal(t, 12, SnapshotInventoryFreshHours())
}
