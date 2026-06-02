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

func TestValidateIdleDetectionUpdate_RejectsUnknownField(t *testing.T) {
	err := validateIdleDetectionUpdate(json.RawMessage(`{"foo": true}`))
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "unknown field")
}

func TestValidateIdleDetectionUpdate_AcceptsZombieThresholds(t *testing.T) {
	body := `{"idle_detection":{"thresholds":{"zombie_cpu_millicores":5,"zombie_peak_millicores":50}}}`
	err := validateIdleDetectionUpdate(json.RawMessage(body))
	assert.NoError(t, err)
}

func TestValidateIdleDetectionUpdate_RejectsZombieOutOfRange(t *testing.T) {
	body := `{"idle_detection":{"thresholds":{"zombie_cpu_millicores":200}}}`
	err := validateIdleDetectionUpdate(json.RawMessage(body))
	assert.Error(t, err)
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

func TestResolveIdleSettings_EnvOverridesDB(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_IDLE_CPU_UTILIZATION_PCT", "5")
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-idle-settings-env-db"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'idle_detection', '{"cpu_utilization_percent": 3}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	settings, err := resolveIdleDetectionSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, int64(5), settings.Thresholds.CPUUtilizationPercent)
}

func TestResolveIdleSettings_NoEnv_DBWins(t *testing.T) {
	config.ResetForTest()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-idle-settings-no-env-db"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'idle_detection', '{"cpu_utilization_percent": 4}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	settings, err := resolveIdleDetectionSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, int64(4), settings.Thresholds.CPUUtilizationPercent)
}
