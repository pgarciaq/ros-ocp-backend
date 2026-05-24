package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestValidateSizingThresholdUpdate_ValidInput(t *testing.T) {
	current := DefaultContainerSizingThresholds()
	err := validateSizingThresholdUpdate(SizingThresholdSettingsUpdate{
		CPUCostPercentile: ptrFloat64(0.71),
		MinMargin:         ptrFloat64(1.2),
		MaxMargin:         ptrFloat64(1.5),
	}, current)
	require.NoError(t, err)
}

func TestValidateSizingThresholdUpdate_OutOfRange(t *testing.T) {
	current := DefaultContainerSizingThresholds()
	err := validateSizingThresholdUpdate(SizingThresholdSettingsUpdate{
		CPUCostPercentile: ptrFloat64(1.5),
		CPUFloorMC:        ptrInt64(0),
	}, current)
	require.Error(t, err)

	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Len(t, valErr.Errors, 2)
	assert.Contains(t, valErr.Error(), "cpu_cost_percentile")
	assert.Contains(t, valErr.Error(), "cpu_floor_mc")
}

func TestValidateSizingThresholdUpdate_MinMarginGreaterThanMax(t *testing.T) {
	current := DefaultContainerSizingThresholds()
	err := validateSizingThresholdUpdate(SizingThresholdSettingsUpdate{
		MinMargin: ptrFloat64(2.0),
		MaxMargin: ptrFloat64(1.5),
	}, current)
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "min_margin")
}

func TestValidateSizingThresholdUpdate_MinMarginAgainstCurrentMax(t *testing.T) {
	current := DefaultContainerSizingThresholds()
	err := validateSizingThresholdUpdate(SizingThresholdSettingsUpdate{
		MinMargin: ptrFloat64(2.0),
	}, current)
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "min_margin")
}

func TestValidateGPUThresholdUpdate_ConfidenceTierOrdering(t *testing.T) {
	current := DefaultGPUThresholdSettings()
	err := validateGPUThresholdUpdate(GPUThresholdSettingsUpdate{
		ConfidenceDaysTier1: ptrInt(10),
		ConfidenceDaysTier2: ptrInt(5),
	}, current)
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "confidence_days_tier1")
}

func TestValidateGPUThresholdUpdate_TimeslicingReplicaOrdering(t *testing.T) {
	current := DefaultGPUThresholdSettings()
	err := validateGPUThresholdUpdate(GPUThresholdSettingsUpdate{
		TimeslicingMinReplicas: ptrInt(10),
		TimeslicingMaxReplicas: ptrInt(4),
	}, current)
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "timeslicing_min_replicas")
}

func TestValidatePVCThresholdUpdate_OversizedNearFullOrdering(t *testing.T) {
	current := DefaultPVCThresholdSettings()
	err := validatePVCThresholdUpdate(PVCThresholdSettingsUpdate{
		OversizedThreshold: ptrFloat64(0.90),
		NearFullThreshold:  ptrFloat64(0.85),
	}, current)
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "oversized_threshold")
}

func TestValidateThresholdSettingsUpdate_RejectsLockedFieldAfterValidation(t *testing.T) {
	t.Setenv("ROS_CONTAINER_CPU_COST_PERCENTILE", "0.65")
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-validate-before-lock"

	// Invalid value on locked field should fail validation (400), not locked (403).
	err := ValidateThresholdSettingsUpdate(ctx, pool, orgID, "container", json.RawMessage(`{"cpu_cost_percentile": 5.0}`))
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.False(t, errors.Is(err, ErrFieldsLocked))
}

func TestUpdateThresholdSettings_ValidationBeforeLockedFields(t *testing.T) {
	t.Setenv("ROS_CONTAINER_CPU_COST_PERCENTILE", "0.65")
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-update-validate-first"

	err := UpdateThresholdSettings(ctx, pool, orgID, "container", json.RawMessage(`{"cpu_cost_percentile": 5.0}`))
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
}

func ptrFloat64(v float64) *float64 { return &v }
func ptrInt64(v int64) *int64     { return &v }
func ptrInt(v int) *int           { return &v }
