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

func TestValidateQuotaSettingsUpdate_RejectsHeadroomOver100(t *testing.T) {
	body := `{"headroom_percent": 101, "high_risk_threshold_percent": 90, "medium_risk_threshold_percent": 70}`
	err := validateQuotaSettingsUpdate(json.RawMessage(body))
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
}

func TestValidateQuotaSettingsUpdate_RejectsMediumNotLessThanHigh(t *testing.T) {
	body := `{"headroom_percent": 10, "high_risk_threshold_percent": 70, "medium_risk_threshold_percent": 80}`
	err := validateQuotaSettingsUpdate(json.RawMessage(body))
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Error(), "high_risk_threshold_percent")
}

func TestValidateQuotaSettingsUpdate_AcceptsValid(t *testing.T) {
	body := `{"headroom_percent": 15, "high_risk_threshold_percent": 85, "medium_risk_threshold_percent": 65}`
	err := validateQuotaSettingsUpdate(json.RawMessage(body))
	require.NoError(t, err)
}

func TestResolveQuotaSettings_DefaultsWithoutDB(t *testing.T) {
	config.ResetForTest()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-quota-settings-defaults"

	settings, err := ResolveQuotaSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 10, settings.HeadroomPercent)
	assert.Equal(t, 90, settings.HighRiskThresholdPercent)
	assert.Equal(t, 70, settings.MediumRiskThresholdPercent)
}

func TestResolveQuotaSettings_DBOverride(t *testing.T) {
	config.ResetForTest()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-quota-settings-db"

	err := UpdateQuotaSettings(ctx, pool, orgID, json.RawMessage(
		`{"headroom_percent": 20, "high_risk_threshold_percent": 88, "medium_risk_threshold_percent": 60}`,
	))
	require.NoError(t, err)

	settings, err := ResolveQuotaSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 20, settings.HeadroomPercent)
	assert.Equal(t, 88, settings.HighRiskThresholdPercent)
	assert.Equal(t, 60, settings.MediumRiskThresholdPercent)

	cfg, err := ResolveQuotaRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 12000, cfg.HeadroomBasisPoints)
	assert.Equal(t, 8800, cfg.HighRiskThresholdBP)
	assert.Equal(t, 6000, cfg.MediumRiskThresholdBP)
}

func TestDeleteQuotaSettings_RestoresDefaults(t *testing.T) {
	config.ResetForTest()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-quota-settings-delete"

	require.NoError(t, UpdateQuotaSettings(ctx, pool, orgID, json.RawMessage(
		`{"headroom_percent": 25, "high_risk_threshold_percent": 80, "medium_risk_threshold_percent": 55}`,
	)))
	require.NoError(t, DeleteQuotaSettings(ctx, pool, orgID))

	settings, err := ResolveQuotaSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 10, settings.HeadroomPercent)
	assert.Equal(t, 90, settings.HighRiskThresholdPercent)
	assert.Equal(t, 70, settings.MediumRiskThresholdPercent)
}

func TestResolveQuotaSettings_EnvOverlayWhenNoDB(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_QUOTA_HEADROOM_PERCENT", "5")
	t.Setenv("ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT", "95")
	t.Setenv("ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT", "75")
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-quota-settings-env"

	settings, err := ResolveQuotaSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 5, settings.HeadroomPercent)
	assert.Equal(t, 95, settings.HighRiskThresholdPercent)
	assert.Equal(t, 75, settings.MediumRiskThresholdPercent)
}

func TestResolveQuotaSettings_EnvOverridesDB(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_QUOTA_HEADROOM_PERCENT", "25")
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-quota-settings-env-db"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'quota', '{"headroom_percent": 20}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	settings, err := ResolveQuotaSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 25, settings.HeadroomPercent)
}

func TestResolveQuotaSettings_NoEnv_DBWins(t *testing.T) {
	config.ResetForTest()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-quota-settings-no-env-db"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'quota', '{"headroom_percent": 22}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	settings, err := ResolveQuotaSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 22, settings.HeadroomPercent)
}
