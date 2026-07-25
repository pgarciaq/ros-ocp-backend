package engine_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestValidateVMSettingsResponse_RejectsInvalidPercentile(t *testing.T) {
	resp := vm.VMSettingsResponseFromConfig(vm.DefaultVMRecConfig())
	resp.Thresholds.CPUPercentileCost = 2.0

	err := vm.ValidateVMSettingsResponse(resp)
	require.Error(t, err)
	var valErr *engine.ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "cpu_percentile_cost")
}

func TestValidateVMSettingsResponse_RejectsNegativeMargin(t *testing.T) {
	resp := vm.VMSettingsResponseFromConfig(vm.DefaultVMRecConfig())
	resp.Thresholds.CPUMarginMin = -0.1

	err := vm.ValidateVMSettingsResponse(resp)
	require.Error(t, err)
}

func TestValidateVMSettingsResponse_RejectsMinGreaterThanMaxMargin(t *testing.T) {
	resp := vm.VMSettingsResponseFromConfig(vm.DefaultVMRecConfig())
	resp.Thresholds.CPUMarginMin = 0.50
	resp.Thresholds.CPUMarginMax = 0.15

	err := vm.ValidateVMSettingsResponse(resp)
	require.Error(t, err)
	var valErr *engine.ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "cpu_margin_min")
}

func TestValidateVMSettingsResponse_AcceptsDefaults(t *testing.T) {
	resp := vm.VMSettingsResponseFromConfig(vm.DefaultVMRecConfig())
	require.NoError(t, vm.ValidateVMSettingsResponse(resp))
	assert.Equal(t, int32(1), resp.MemoryFloors.LinuxGiB)
	assert.Equal(t, int32(2), resp.MemoryFloors.WindowsGiB)
}

func TestValidateVMSettingsResponse_RejectsLowMemoryFloor(t *testing.T) {
	resp := vm.VMSettingsResponseFromConfig(vm.DefaultVMRecConfig())
	resp.MemoryFloors.LinuxGiB = 0

	err := vm.ValidateVMSettingsResponse(resp)
	require.Error(t, err)
	var valErr *engine.ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "linux_gib")
}

func TestUpdateVMSettings_RejectsInvalidPercentile(t *testing.T) {
	err := vm.ValidateVMSettingsUpdate(json.RawMessage(`{"thresholds": {"cpu_percentile_cost": 2.0}}`))
	require.NoError(t, err)

	resp := vm.VMSettingsResponseFromConfig(vm.DefaultVMRecConfig())
	resp.Thresholds.CPUPercentileCost = 2.0
	err = vm.ValidateVMSettingsResponse(resp)
	require.Error(t, err)
}

func TestResolveVMSettings_EnvOverridesDB(t *testing.T) {
	saved := vm.DefaultVMRecConfigVar
	t.Cleanup(func() { vm.DefaultVMRecConfigVar = saved })

	t.Setenv("ROS_VM_CPU_PERCENTILE_COST", "0.88")
	config.ResetForTest()
	vm.InitVMRecDefaults(config.GetConfig())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-settings-env"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'vm', '{"thresholds": {"cpu_percentile_cost": 0.72}}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := vm.ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.88, got.CPUPercentileCost, 1e-9)
}

func TestGetVMSettingsForAPI_IncludesCPUAdaptiveMarginEnabled(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-adaptive-api"

	resp, err := vm.GetVMSettingsForAPI(ctx, pool, orgID)
	require.NoError(t, err)
	assert.True(t, resp.CPUAdaptiveMarginEnabled)
}

func TestGetVMSettingsForAPI_IncludesHistoryRetentionDays(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-history-retention-api"

	resp, err := vm.GetVMSettingsForAPI(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 90, resp.HistoryRetentionDays)

	t.Setenv("ROS_VM_REC_HISTORY_RETENTION_DAYS", "45")
	config.ResetForTest()

	resp, err = vm.GetVMSettingsForAPI(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 45, resp.HistoryRetentionDays)
}

func TestUpdateVMSettings_CPUAdaptiveMarginEnabled(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-adaptive-put"

	require.NoError(t, vm.UpdateVMSettings(ctx, pool, orgID,
		json.RawMessage(`{"cpu_adaptive_margin_enabled": false}`)))

	got, err := vm.GetVMSettingsForAPI(ctx, pool, orgID)
	require.NoError(t, err)
	assert.False(t, got.CPUAdaptiveMarginEnabled)
}

func TestResolveVMSettings_EnvLocksCPUAdaptiveMargin(t *testing.T) {
	saved := vm.DefaultVMRecConfigVar
	t.Cleanup(func() { vm.DefaultVMRecConfigVar = saved })

	t.Setenv("ROS_VM_CPU_ADAPTIVE_MARGIN_ENABLED", "false")
	config.ResetForTest()
	vm.InitVMRecDefaults(config.GetConfig())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-adaptive-env"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'vm', '{"cpu_adaptive_margin_enabled": true}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := vm.ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.False(t, got.CPUAdaptiveMarginEnabled)

	resp, err := vm.GetVMSettingsForAPI(ctx, pool, orgID)
	require.NoError(t, err)
	assert.False(t, resp.CPUAdaptiveMarginEnabled)
	assert.Contains(t, resp.LockedFields, "cpu_adaptive_margin_enabled")
}

func TestGetVMSettingsForAPI_IncludesGPUClassificationThresholds(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-gpu-classification-api"

	resp, err := vm.GetVMSettingsForAPI(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, int32(500), resp.GPU.IdleThresholdBP)
	assert.Equal(t, int32(3000), resp.GPU.UnderutilThresholdBP)
	assert.Equal(t, int32(0), resp.GPU.FBSaturationMiB)
	assert.Equal(t, int32(8500), resp.GPU.ComputeSaturationThresholdBP)
}

func TestUpdateVMSettings_GPUClassificationThresholds(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-gpu-classification-put"

	require.NoError(t, vm.UpdateVMSettings(ctx, pool, orgID, json.RawMessage(`{
		"gpu": {
			"idle_threshold_bp": 800,
			"underutil_threshold_bp": 4000,
			"fb_saturation_mib": 16384,
			"compute_saturation_threshold_bp": 9000
		}
	}`)))

	got, err := vm.ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.08, got.GPUIdleThreshold, 1e-9)
	assert.InDelta(t, 0.40, got.GPUUnderutilThreshold, 1e-9)
	assert.InDelta(t, 16384, got.GPUFBSaturationMiB, 1e-9)
	assert.InDelta(t, 0.90, got.GPUComputeSaturationThreshold, 1e-9)

	resp, err := vm.GetVMSettingsForAPI(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, int32(800), resp.GPU.IdleThresholdBP)
	assert.Equal(t, int32(4000), resp.GPU.UnderutilThresholdBP)
	assert.Equal(t, int32(16384), resp.GPU.FBSaturationMiB)
	assert.Equal(t, int32(9000), resp.GPU.ComputeSaturationThresholdBP)
}

func TestValidateVMSettingsResponse_RejectsInvalidGPUClassificationOrder(t *testing.T) {
	resp := vm.VMSettingsResponseFromConfig(vm.DefaultVMRecConfig())
	resp.GPU.IdleThresholdBP = 5000
	resp.GPU.UnderutilThresholdBP = 3000

	err := vm.ValidateVMSettingsResponse(resp)
	require.Error(t, err)
	var valErr *engine.ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "idle_threshold_bp")
}

func TestResolveVMSettings_EnvLocksGPUClassification(t *testing.T) {
	saved := vm.DefaultVMRecConfigVar
	t.Cleanup(func() { vm.DefaultVMRecConfigVar = saved })

	t.Setenv("ROS_VM_GPU_IDLE_THRESHOLD", "0.10")
	config.ResetForTest()
	vm.InitVMRecDefaults(config.GetConfig())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-gpu-classification-env"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'vm', '{"gpu": {"idle_threshold_bp": 200}}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := vm.ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.10, got.GPUIdleThreshold, 1e-9)

	resp, err := vm.GetVMSettingsForAPI(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, int32(1000), resp.GPU.IdleThresholdBP)
	assert.Contains(t, resp.LockedFields, "gpu.idle_threshold_bp")
}

func TestResolveVMSettings_NoEnv_DBWins(t *testing.T) {
	saved := vm.DefaultVMRecConfigVar
	t.Cleanup(func() { vm.DefaultVMRecConfigVar = saved })

	config.ResetForTest()
	vm.InitVMRecDefaults(config.GetConfig())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-settings-db"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'vm', '{"thresholds": {"cpu_percentile_cost": 0.72}}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got, err := vm.ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got.CPUPercentileCost, 1e-9)
}
