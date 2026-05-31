package engine

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
}

func TestUpdateVMSettings_RejectsInvalidPercentile(t *testing.T) {
	err := validateVMSettingsUpdate(json.RawMessage(`{"thresholds": {"cpu_percentile_cost": 2.0}}`))
	require.NoError(t, err)

	resp := vmSettingsResponseFromConfig(DefaultVMRecConfig())
	resp.Thresholds.CPUPercentileCost = 2.0
	err = validateVMSettingsResponse(resp)
	require.Error(t, err)
}
