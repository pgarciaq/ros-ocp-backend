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
