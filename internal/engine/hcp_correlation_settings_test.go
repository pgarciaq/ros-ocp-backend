package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

const hcpValidBody = `{"h_p99_threshold_s": 0.5, "h_baseline_multiple": 4, "c_cpu_pct": 85, "c_etcd_p99_s": 0.02, "window_h": 2, "skew_m": 10, "freshness_h": 4, "expiry_h": 48, "z_idle_req_per_day": 50, "z_idle_cpu_floor_mc": 25, "z_short_window_days": 5, "z_short_required_days": 3, "z_idle_window_days": 14}`

func TestValidateHCPCorrelationSettingsUpdate_AcceptsValid(t *testing.T) {
	err := validateHCPCorrelationSettingsUpdate(json.RawMessage(hcpValidBody))
	require.NoError(t, err)
}

func TestValidateHCPCorrelationSettingsUpdate_RejectsUnknownField(t *testing.T) {
	err := validateHCPCorrelationSettingsUpdate(json.RawMessage(
		`{"h_p99_threshold_s": 0.5, "h_baseline_multiple": 4, "c_cpu_pct": 85, "c_etcd_p99_s": 0.02, "window_h": 2, "skew_m": 10, "freshness_h": 4, "bogus": 1}`))
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
}

func TestValidateHCPCorrelationSettingsUpdate_RejectsMissingField(t *testing.T) {
	err := validateHCPCorrelationSettingsUpdate(json.RawMessage(`{"h_p99_threshold_s": 0.5}`))
	require.Error(t, err, "PUT replaces the whole domain: partial bodies are rejected like quota")
}

func TestValidateHCPCorrelationSettingsUpdate_RejectsOutOfRange(t *testing.T) {
	err := validateHCPCorrelationSettingsUpdate(json.RawMessage(
		`{"h_p99_threshold_s": 0, "h_baseline_multiple": 4, "c_cpu_pct": 85, "c_etcd_p99_s": 0.02, "window_h": 2, "skew_m": 10, "freshness_h": 4, "expiry_h": 48, "z_idle_req_per_day": 50, "z_idle_cpu_floor_mc": 25, "z_short_window_days": 5, "z_short_required_days": 3, "z_idle_window_days": 14}`))
	require.Error(t, err, "zero threshold is out of range")
}

func TestValidateHCPCorrelationSettingsUpdate_RejectsFreshnessBelowWindow(t *testing.T) {
	err := validateHCPCorrelationSettingsUpdate(json.RawMessage(
		`{"h_p99_threshold_s": 0.5, "h_baseline_multiple": 4, "c_cpu_pct": 85, "c_etcd_p99_s": 0.02, "window_h": 4, "skew_m": 10, "freshness_h": 2, "expiry_h": 48, "z_idle_req_per_day": 50, "z_idle_cpu_floor_mc": 25, "z_short_window_days": 5, "z_short_required_days": 3, "z_idle_window_days": 14}`))
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "freshness_h")
}

func TestValidateHCPCorrelationSettingsUpdate_RejectsZombieOutOfRange(t *testing.T) {
	for _, bad := range []string{"0", "-5", "2000000", `"many"`} {
		body := `{"h_p99_threshold_s": 0.5, "h_baseline_multiple": 4, "c_cpu_pct": 85, "c_etcd_p99_s": 0.02, "window_h": 2, "skew_m": 10, "freshness_h": 4, "expiry_h": 48, "z_idle_req_per_day": ` + bad + `}`
		err := validateHCPCorrelationSettingsUpdate(json.RawMessage(body))
		require.Error(t, err, "z_idle_req_per_day=%s must be a number in [1, 1e6]", bad)
	}
}

func TestResolveHCPCorrelationSettings_DefaultsAndOverride(t *testing.T) {
	config.ResetForTest()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-hcp-settings-defaults"

	s, err := ResolveHCPCorrelationSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, hcpDefaultHP99ThresholdS, s.HP99ThresholdS)
	assert.Equal(t, hcpDefaultWindowHours, s.WindowHours)
	assert.Equal(t, hcpDefaultZombieIdleReqPerDay, s.ZombieIdleReqPerDay)
	assert.Equal(t, hcpDefaultZombieShortWindowDays, s.ZombieShortWindowDays)
	assert.Equal(t, hcpDefaultZombieShortRequiredDays, s.ZombieShortRequiredDays)
	assert.Equal(t, hcpDefaultZombieIdleWindowDays, s.ZombieIdleWindowDays)

	require.NoError(t, UpdateHCPCorrelationSettings(ctx, pool, orgID, json.RawMessage(hcpValidBody)))
	s, err = ResolveHCPCorrelationSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 0.5, s.HP99ThresholdS)
	assert.Equal(t, 2, s.WindowHours)
	assert.Equal(t, 48, s.ExpiryHours)
	assert.Equal(t, 50.0, s.ZombieIdleReqPerDay)
	assert.Equal(t, 5, s.ZombieShortWindowDays)
	assert.Equal(t, 3, s.ZombieShortRequiredDays)
	assert.Equal(t, 14, s.ZombieIdleWindowDays)

	require.NoError(t, DeleteHCPCorrelationSettings(ctx, pool, orgID))
	s, err = ResolveHCPCorrelationSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, hcpDefaultHP99ThresholdS, s.HP99ThresholdS, "delete restores compiled defaults")
}

func TestUpdateHCPCorrelationSettings_RejectsLockedField(t *testing.T) {
	t.Setenv("ROS_HCP_C_CPU_PCT", "90")
	config.ResetForTest()
	_ = config.GetConfig()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	err := UpdateHCPCorrelationSettings(ctx, pool, "org-hcp-settings-locked", json.RawMessage(hcpValidBody))
	require.Error(t, err, "env-locked field in body must 403 at the engine layer")
	assert.ErrorIs(t, err, ErrFieldsLocked)

	s, err := ResolveHCPCorrelationSettings(ctx, pool, "org-hcp-settings-locked")
	require.NoError(t, err)
	assert.Equal(t, 90.0, s.CCPUPct, "locked env value wins over tenant body")
}

func TestUpdateHCPCorrelationSettings_RejectsLockedZombieField(t *testing.T) {
	t.Setenv("ROS_HCP_ZOMBIE_IDLE_REQ_PER_DAY", "250")
	config.ResetForTest()
	_ = config.GetConfig()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	err := UpdateHCPCorrelationSettings(ctx, pool, "org-hcp-settings-zlocked", json.RawMessage(hcpValidBody))
	require.Error(t, err, "env-locked z field in body must 403 at the engine layer")
	assert.ErrorIs(t, err, ErrFieldsLocked)

	s, err := ResolveHCPCorrelationSettings(ctx, pool, "org-hcp-settings-zlocked")
	require.NoError(t, err)
	assert.Equal(t, 250.0, s.ZombieIdleReqPerDay, "locked env value wins over tenant body")
}

func TestIsSettingsLocked_HCPCorrelationOptOut(t *testing.T) {
	t.Setenv("ROS_SETTINGS_LOCKED", "true")
	t.Setenv("ROS_SETTINGS_LOCKED_HCP", "false")
	config.ResetForTest()
	_ = config.GetConfig()
	t.Cleanup(func() {
		config.ResetForTest()
		_ = config.GetConfig()
	})

	assert.False(t, IsSettingsLocked("hcp-correlation"), "opt-out keeps the domain writable under global lock")
	assert.True(t, IsSettingsLocked("quota"), "sibling domains stay locked")
}

func TestValidateHCPCorrelationSettingsUpdate_RejectsShortWindowViolations(t *testing.T) {
	base := `{"h_p99_threshold_s": 0.5, "h_baseline_multiple": 4, "c_cpu_pct": 85, "c_etcd_p99_s": 0.02, "window_h": 2, "skew_m": 10, "freshness_h": 4, "expiry_h": 48, "z_idle_req_per_day": 50, "z_idle_cpu_floor_mc": 25, %s}`
	for _, tc := range []struct {
		name   string
		fields string
		want   string
	}{
		{"required-exceeds-window", `"z_short_window_days": 3, "z_short_required_days": 5, "z_idle_window_days": 14`, "z_short_required_days"},
		{"short-exceeds-idle", `"z_short_window_days": 20, "z_short_required_days": 3, "z_idle_window_days": 14`, "z_short_window_days"},
		{"short-window-zero", `"z_short_window_days": 0, "z_short_required_days": 3, "z_idle_window_days": 14`, "z_short_window_days"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateHCPCorrelationSettingsUpdate(json.RawMessage(fmt.Sprintf(base, tc.fields)))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
