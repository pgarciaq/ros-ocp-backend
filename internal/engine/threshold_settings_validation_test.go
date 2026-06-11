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

func TestValidateThresholdSettingsUpdate_RejectsUnknownField(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-unknown-field"

	err := ValidateThresholdSettingsUpdate(ctx, pool, orgID, "container",
		json.RawMessage(`{"not_a_valid_threshold_field": 0.99}`))
	require.Error(t, err)

	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "unknown field")
}

func TestValidateThresholdSettingsUpdate_RejectsLockedFieldsKey(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-threshold-locked-key"

	err := ValidateThresholdSettingsUpdate(ctx, pool, orgID, "container",
		json.RawMessage(`{"locked_fields": ["cpu_cost_percentile"]}`))
	require.Error(t, err)

	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "unknown field")
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

func TestValidateNamespaceThresholds_ValidInput(t *testing.T) {
	current := DefaultNamespaceSizingThresholds()
	err := validateSizingThresholdUpdate(SizingThresholdSettingsUpdate{
		CPUCostPercentile:      ptrFloat64(0.71),
		MemTrendSlopeThreshold: ptrFloat64(600.0),
		MinMargin:              ptrFloat64(1.2),
		MaxMargin:              ptrFloat64(1.5),
	}, current)
	require.NoError(t, err)
}

func TestValidateNamespaceThresholds_InvalidPercentile(t *testing.T) {
	current := DefaultNamespaceSizingThresholds()
	err := validateSizingThresholdUpdate(SizingThresholdSettingsUpdate{
		CPUCostPercentile: ptrFloat64(1.5),
	}, current)
	require.Error(t, err)

	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "cpu_cost_percentile")
}

func TestValidateNodeThresholds_ValidInput(t *testing.T) {
	err := validateNodeThresholdUpdate(NodeThresholdSettingsUpdate{
		UnderutilThreshold:    ptrFloat64(0.25),
		OvercommitThreshold:   ptrFloat64(1.75),
		CostTargetUtilization: ptrFloat64(0.75),
		TrendMinDays:          ptrInt(5),
	}, DefaultNodeThresholdSettings())
	require.NoError(t, err)
}

func TestValidateNodeThresholds_InvalidUtilization(t *testing.T) {
	err := validateNodeThresholdUpdate(NodeThresholdSettingsUpdate{
		CostTargetUtilization: ptrFloat64(1.5),
		UnderutilThreshold:    ptrFloat64(0.0),
	}, DefaultNodeThresholdSettings())
	require.Error(t, err)

	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "cost_target_utilization")
	assert.Contains(t, valErr.Error(), "underutil_threshold")
}

func TestValidateNodeThresholds_OvercommitBelowUtilization(t *testing.T) {
	err := validateNodeThresholdUpdate(NodeThresholdSettingsUpdate{
		OvercommitThreshold: ptrFloat64(0.80),
	}, DefaultNodeThresholdSettings())
	require.Error(t, err)

	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "overcommit_threshold")
}

func TestValidateNodeThresholds_PodHeadroomValid(t *testing.T) {
	err := validateNodeThresholdUpdate(NodeThresholdSettingsUpdate{
		PodHeadroomConsolidationGate:     ptrFloat64(0.20),
		PodHeadroomNotificationThreshold: ptrFloat64(0.08),
	}, DefaultNodeThresholdSettings())
	require.NoError(t, err)
}

func TestValidateNodeThresholds_PodHeadroomOutOfRange(t *testing.T) {
	err := validateNodeThresholdUpdate(NodeThresholdSettingsUpdate{
		PodHeadroomConsolidationGate: ptrFloat64(1.5),
	}, DefaultNodeThresholdSettings())
	require.Error(t, err)

	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "pod_headroom_consolidation_gate")

	err = validateNodeThresholdUpdate(NodeThresholdSettingsUpdate{
		PodHeadroomNotificationThreshold: ptrFloat64(-0.01),
	}, DefaultNodeThresholdSettings())
	require.Error(t, err)
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "pod_headroom_notification_threshold")
}

func TestValidateNodeThresholds_PodHeadroomGateBelowNotification(t *testing.T) {
	err := validateNodeThresholdUpdate(NodeThresholdSettingsUpdate{
		PodHeadroomConsolidationGate:     ptrFloat64(0.05),
		PodHeadroomNotificationThreshold: ptrFloat64(0.10),
	}, DefaultNodeThresholdSettings())
	require.Error(t, err)

	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "pod_headroom_consolidation_gate")
}

func TestValidateNodeThresholds_IdleZombieValid(t *testing.T) {
	err := validateNodeThresholdUpdate(NodeThresholdSettingsUpdate{
		ZombieCPUP95MC:   ptrInt64(150),
		ZombieMaxPods:    ptrInt64(3),
		IdleCPUUtilPct:   ptrInt64(8),
		IdleMemUtilPct:   ptrInt64(12),
		IdleMaxPods:      ptrInt64(8),
	}, DefaultNodeThresholdSettings())
	require.NoError(t, err)
}

func TestValidateNodeThresholds_IdleZombieOutOfRange(t *testing.T) {
	err := validateNodeThresholdUpdate(NodeThresholdSettingsUpdate{
		ZombieCPUP95MC: ptrInt64(0),
	}, DefaultNodeThresholdSettings())
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "zombie_cpu_p95_mc")

	err = validateNodeThresholdUpdate(NodeThresholdSettingsUpdate{
		IdleCPUUtilPct: ptrInt64(101),
	}, DefaultNodeThresholdSettings())
	require.Error(t, err)
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "idle_cpu_util_pct")

	err = validateNodeThresholdUpdate(NodeThresholdSettingsUpdate{
		ZombieMaxPods: ptrInt64(0),
	}, DefaultNodeThresholdSettings())
	require.Error(t, err)
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "zombie_max_pods")
}

func TestValidateSizingThresholdUpdate_SparseDataThresholdValid(t *testing.T) {
	current := DefaultContainerSizingThresholds()
	for _, value := range []int{1, 15, 30} {
		err := validateSizingThresholdUpdate(SizingThresholdSettingsUpdate{
			SparseDataThreshold: ptrInt(value),
		}, current)
		require.NoError(t, err, "sparse_data_threshold=%d should be valid", value)
	}
}

func TestValidateSizingThresholdUpdate_SparseDataThresholdOutOfRange(t *testing.T) {
	current := DefaultContainerSizingThresholds()
	for _, value := range []int{0, 31, -1} {
		err := validateSizingThresholdUpdate(SizingThresholdSettingsUpdate{
			SparseDataThreshold: ptrInt(value),
		}, current)
		require.Error(t, err, "sparse_data_threshold=%d should be rejected", value)

		var valErr *ThresholdValidationError
		require.ErrorAs(t, err, &valErr)
		assert.Contains(t, valErr.Error(), "sparse_data_threshold")
	}
}

func ptrFloat64(v float64) *float64 { return &v }
func ptrInt64(v int64) *int64       { return &v }
func ptrInt(v int) *int             { return &v }
