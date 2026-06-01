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

func TestClusterQuotaRecConfigFromSettings(t *testing.T) {
	cfg := clusterQuotaRecConfigFromSettings(ClusterQuotaSettings{
		HeadroomPercent:            10,
		HighRiskThresholdPercent:   90,
		MediumRiskThresholdPercent: 70,
	})
	assert.Equal(t, 11000, cfg.HeadroomBasisPoints)
	assert.Equal(t, 9000, cfg.HighRiskThresholdBP)
	assert.Equal(t, 7000, cfg.MediumRiskThresholdBP)
}

func TestValidateClusterQuotaSettingsUpdate(t *testing.T) {
	raw := json.RawMessage(`{"headroom_percent":10,"high_risk_threshold_percent":90,"medium_risk_threshold_percent":70}`)
	require.NoError(t, validateClusterQuotaSettingsUpdate(raw))

	bad := json.RawMessage(`{"headroom_percent":10,"high_risk_threshold_percent":50,"medium_risk_threshold_percent":70}`)
	assert.Error(t, validateClusterQuotaSettingsUpdate(bad))
}

func TestClusterQuotaSettingsFromConfig(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_CLUSTER_QUOTA_HEADROOM_PERCENT", "15")
	_ = config.GetConfig()
	s := clusterQuotaSettingsFromConfig(config.GetConfig())
	assert.Equal(t, 15, s.HeadroomPercent)
}

func TestResolveClusterQuotaSettings_EnvOverridesDB(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_CLUSTER_QUOTA_HEADROOM_PERCENT", "25")
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-cluster-quota-env-db"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'cluster-quota', '{"headroom_percent": 20}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	settings, err := ResolveClusterQuotaSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 25, settings.HeadroomPercent)
}

func TestResolveClusterQuotaSettings_NoEnv_DBWins(t *testing.T) {
	config.ResetForTest()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-cluster-quota-no-env-db"

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'cluster-quota', '{"headroom_percent": 22}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	settings, err := ResolveClusterQuotaSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 22, settings.HeadroomPercent)
}

func TestUpdateClusterQuotaSettings_RejectsLockedField(t *testing.T) {
	t.Setenv("ROS_CLUSTER_QUOTA_HEADROOM_PERCENT", "12")
	config.ResetForTest()
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-cluster-quota-locked"

	err := UpdateClusterQuotaSettings(ctx, pool, orgID, json.RawMessage(
		`{"headroom_percent": 20, "high_risk_threshold_percent": 90, "medium_risk_threshold_percent": 70}`,
	))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFieldsLocked)
}
