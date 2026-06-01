package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestValidateVMSettingsResponse_RejectsInvalidPercentile(t *testing.T) {
	resp := vmSettingsResponseFromConfig(DefaultVMRecConfig())
	resp.Thresholds.CPUPercentileCost = 2.0

	err := validateVMSettingsResponse(resp)
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "cpu_percentile_cost")
}

func TestValidateVMSettingsResponse_RejectsNegativeMargin(t *testing.T) {
	resp := vmSettingsResponseFromConfig(DefaultVMRecConfig())
	resp.Thresholds.CPUMarginMin = -0.1

	err := validateVMSettingsResponse(resp)
	require.Error(t, err)
}

func TestValidateVMSettingsResponse_RejectsMinGreaterThanMaxMargin(t *testing.T) {
	resp := vmSettingsResponseFromConfig(DefaultVMRecConfig())
	resp.Thresholds.CPUMarginMin = 0.50
	resp.Thresholds.CPUMarginMax = 0.15

	err := validateVMSettingsResponse(resp)
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "cpu_margin_min")
}

func TestValidateVMSettingsResponse_AcceptsDefaults(t *testing.T) {
	resp := vmSettingsResponseFromConfig(DefaultVMRecConfig())
	require.NoError(t, validateVMSettingsResponse(resp))
	assert.Equal(t, int32(1), resp.MemoryFloors.LinuxGiB)
	assert.Equal(t, int32(2), resp.MemoryFloors.WindowsGiB)
}

func TestValidateVMSettingsResponse_RejectsLowMemoryFloor(t *testing.T) {
	resp := vmSettingsResponseFromConfig(DefaultVMRecConfig())
	resp.MemoryFloors.LinuxGiB = 0

	err := validateVMSettingsResponse(resp)
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "linux_gib")
}

func TestUpdateVMSettings_RejectsInvalidPercentile(t *testing.T) {
	err := validateVMSettingsUpdate(json.RawMessage(`{"thresholds": {"cpu_percentile_cost": 2.0}}`))
	require.NoError(t, err)

	resp := vmSettingsResponseFromConfig(DefaultVMRecConfig())
	resp.Thresholds.CPUPercentileCost = 2.0
	err = validateVMSettingsResponse(resp)
	require.Error(t, err)
}

func TestResolveVMSettings_EnvOverridesDB(t *testing.T) {
	saved := defaultVMRecConfig
	t.Cleanup(func() { defaultVMRecConfig = saved })

	t.Setenv("ROS_VM_CPU_PERCENTILE_COST", "0.88")
	config.ResetForTest()
	InitVMRecDefaults(config.GetConfig())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-settings-env"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'vm', '{"thresholds": {"cpu_percentile_cost": 0.72}}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.88, got.CPUPercentileCost, 1e-9)
}

func TestGetVMSettingsForAPI_IncludesCPUAdaptiveMarginEnabled(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-adaptive-api"

	resp, err := GetVMSettingsForAPI(ctx, pool, orgID)
	require.NoError(t, err)
	assert.True(t, resp.CPUAdaptiveMarginEnabled)
}

func TestUpdateVMSettings_CPUAdaptiveMarginEnabled(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-adaptive-put"

	require.NoError(t, UpdateVMSettings(ctx, pool, orgID,
		json.RawMessage(`{"cpu_adaptive_margin_enabled": false}`)))

	got, err := GetVMSettingsForAPI(ctx, pool, orgID)
	require.NoError(t, err)
	assert.False(t, got.CPUAdaptiveMarginEnabled)
}

func TestResolveVMSettings_EnvLocksCPUAdaptiveMargin(t *testing.T) {
	saved := defaultVMRecConfig
	t.Cleanup(func() { defaultVMRecConfig = saved })

	t.Setenv("ROS_VM_CPU_ADAPTIVE_MARGIN_ENABLED", "false")
	config.ResetForTest()
	InitVMRecDefaults(config.GetConfig())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-adaptive-env"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'vm', '{"cpu_adaptive_margin_enabled": true}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.False(t, got.CPUAdaptiveMarginEnabled)

	resp, err := GetVMSettingsForAPI(ctx, pool, orgID)
	require.NoError(t, err)
	assert.False(t, resp.CPUAdaptiveMarginEnabled)
	assert.Contains(t, resp.LockedFields, "cpu_adaptive_margin_enabled")
}

func TestResolveVMSettings_NoEnv_DBWins(t *testing.T) {
	saved := defaultVMRecConfig
	t.Cleanup(func() { defaultVMRecConfig = saved })

	config.ResetForTest()
	InitVMRecDefaults(config.GetConfig())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-settings-db"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'vm', '{"thresholds": {"cpu_percentile_cost": 0.72}}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got.CPUPercentileCost, 1e-9)
}
