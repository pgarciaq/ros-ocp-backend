package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestQuotaSettingsCache_HitsOnSecondCall(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-quota-settings-cache-hit"

	config.ResetForTest()
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'quota', '{"headroom_percent": 22}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got1, err := ResolveQuotaSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 22, got1.HeadroomPercent)

	_, err = pool.Exec(ctx, `
		UPDATE recommendation_thresholds
		SET thresholds = '{"headroom_percent": 33}'::jsonb
		WHERE org_id = $1 AND recommendation_type = 'quota'`, orgID)
	require.NoError(t, err)

	got2, err := ResolveQuotaSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, 22, got2.HeadroomPercent, "second call should return cached value without re-reading DB")
}

func TestIdleSettingsCache_HitsOnSecondCall(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-idle-settings-cache-hit"

	config.ResetForTest()
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'idle_detection', '{"cpu_utilization_percent": 7}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got1, err := resolveIdleDetectionSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, int64(7), got1.Thresholds.CPUUtilizationPercent)

	_, err = pool.Exec(ctx, `
		UPDATE recommendation_thresholds
		SET thresholds = '{"cpu_utilization_percent": 11}'::jsonb
		WHERE org_id = $1 AND recommendation_type = 'idle_detection'`, orgID)
	require.NoError(t, err)

	got2, err := resolveIdleDetectionSettings(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Equal(t, int64(7), got2.Thresholds.CPUUtilizationPercent, "second call should return cached value without re-reading DB")
}
