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

func TestResolveNodeThresholds_PodHeadroomEnvOverridesDefault(t *testing.T) {
	t.Setenv("ROS_NODE_POD_HEADROOM_CONSOLIDATION_GATE", "0.18")
	t.Setenv("ROS_NODE_POD_HEADROOM_NOTIFICATION_THRESHOLD", "0.07")
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-node-pod-headroom-env"

	got, err := ResolveNodeThresholdSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.18, got.PodHeadroomConsolidationGate, 1e-9)
	assert.InDelta(t, 0.07, got.PodHeadroomNotificationThreshold, 1e-9)
}

func TestResolveNodeThresholds_EnvOverridesDefault(t *testing.T) {
	t.Setenv("ROS_NODE_COST_TARGET_UTILIZATION", "0.72")
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-node-env"

	got, err := ResolveNodeThresholdSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got.CostTargetUtilization, 1e-9)
	assert.InDelta(t, DefaultNodeThresholdSettings().UnderutilThreshold, got.UnderutilThreshold, 1e-9)
}

func TestResolveNodeThresholds_DBOverridesDefault(t *testing.T) {
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-node-db"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'node', '{"cost_target_utilization": 0.71}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := ResolveNodeThresholdSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.71, got.CostTargetUtilization, 1e-9)
	assert.InDelta(t, DefaultNodeThresholdSettings().OvercommitThreshold, got.OvercommitThreshold, 1e-9)
}

func TestResolveNodeThresholds_EnvLocksDBOverride(t *testing.T) {
	t.Setenv("ROS_NODE_COST_TARGET_UTILIZATION", "0.88")
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-node-env-lock"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'node', '{"cost_target_utilization": 0.71}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := ResolveNodeThresholdSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.88, got.CostTargetUtilization, 1e-9)

	resp, err := GetThresholdSettingsForAPI(ctx, pool, orgID, "node")
	require.NoError(t, err)
	settings, ok := resp.(NodeThresholdSettingsResponse)
	require.True(t, ok)
	assert.Contains(t, settings.LockedFields, "cost_target_utilization")
}

func TestResolveGPUThresholds_EnvOverridesDefault(t *testing.T) {
	t.Setenv("ROS_GPU_IDLE_THRESHOLD", "0.08")
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-gpu-env"

	got, err := ResolveGPUThresholdSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.08, got.IdleThreshold, 1e-9)
	assert.InDelta(t, DefaultGPUThresholdSettings().UnderutilizedSM, got.UnderutilizedSM, 1e-9)
}

func TestResolveGPUThresholds_DBOverridesDefault(t *testing.T) {
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-gpu-db"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'gpu', '{"idle_threshold": 0.06}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := ResolveGPUThresholdSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.06, got.IdleThreshold, 1e-9)
	assert.InDelta(t, DefaultGPUThresholdSettings().MIGFBPercentile, got.MIGFBPercentile, 1e-9)
}

func TestResolvePVCThresholds_EnvOverridesDefault(t *testing.T) {
	t.Setenv("ROS_PVC_OVERSIZED_THRESHOLD", "0.28")
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-pvc-env"

	got, err := ResolvePVCThresholdSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.28, got.OversizedThreshold, 1e-9)
	assert.InDelta(t, DefaultPVCThresholdSettings().NearFullThreshold, got.NearFullThreshold, 1e-9)
}

func TestResolvePVCThresholds_DBOverridesDefault(t *testing.T) {
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-pvc-db"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'pvc', '{"oversized_threshold": 0.33}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := ResolvePVCThresholdSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.33, got.OversizedThreshold, 1e-9)
	assert.Equal(t, DefaultPVCThresholdSettings().MinTrendDays, got.MinTrendDays)
}

func TestResolveNamespaceThresholds_EnvOverridesDefault(t *testing.T) {
	t.Setenv("ROS_NAMESPACE_CPU_COST_PERCENTILE", "0.77")
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-namespace-env"

	got, err := ResolveNamespaceSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.77, got.CPUCostPercentile, 1e-9)
	assert.InDelta(t, DefaultNamespaceSizingThresholds().MemTrendSlopeThreshold, got.MemTrendSlopeThreshold, 1e-9)
}

func TestResolveNamespaceThresholds_DBOverridesDefault(t *testing.T) {
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-namespace-db"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'namespace', '{"cpu_cost_percentile": 0.69}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := ResolveNamespaceSizingThresholds(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.69, got.CPUCostPercentile, 1e-9)
	assert.InDelta(t, DefaultNamespaceSizingThresholds().MemCostPercentile, got.MemCostPercentile, 1e-9)
}
