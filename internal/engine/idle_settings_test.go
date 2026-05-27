package engine

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateIdleDetectionUpdate_RejectsUnknownField(t *testing.T) {
	err := validateIdleDetectionUpdate(json.RawMessage(`{"foo": true}`))
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "unknown field")
}

func TestValidateIdleDetectionUpdate_AcceptsThresholds(t *testing.T) {
	body := `{
		"idle_detection": {
			"enabled": true,
			"thresholds": {
				"cpu_utilization_percent": 3,
				"memory_utilization_percent": 4
			}
		}
	}`
	err := validateIdleDetectionUpdate(json.RawMessage(body))
	require.NoError(t, err)
}

func TestValidateIdleDetectionUpdate_RejectsInvalidWorkloadType(t *testing.T) {
	body := `{
		"idle_detection": {
			"exclusions": {
				"workload_types": ["NotARealKind"]
			}
		}
	}`
	err := validateIdleDetectionUpdate(json.RawMessage(body))
	require.Error(t, err)
}

func TestGpuIdleConfigFromSettings(t *testing.T) {
	cfg := gpuIdleConfigFromSettings(IdleDetectionSettings{
		Enabled: true,
		Thresholds: IdleDetectionThresholds{
			GPUSMActiveBasisPoints:   400,
			GPUDRAMActiveBasisPoints: 450,
			MinimumObservationDays:   10,
		},
	})
	assert.Equal(t, int64(400), cfg.IdleSMActiveBP)
	assert.Equal(t, 10, cfg.MinObservationDays)
}
