package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestVMSettingsCache_HitsOnSecondCall(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-settings-cache-hit"

	config.ResetForTest()
	vm.InitVMRecDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'vm', '{"thresholds": {"cpu_percentile_cost": 0.72}}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got1, err := ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got1.CPUPercentileCost, 1e-9)

	_, err = pool.Exec(ctx, `
		UPDATE recommendation_thresholds
		SET thresholds = '{"thresholds": {"cpu_percentile_cost": 0.55}}'::jsonb
		WHERE org_id = $1 AND recommendation_type = 'vm'`, orgID)
	require.NoError(t, err)

	got2, err := ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got2.CPUPercentileCost, 1e-9, "second call should return cached value without re-reading DB")
}

func TestVMSettingsCache_InvalidatedOnPUT(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-settings-cache-put"

	config.ResetForTest()
	vm.InitVMRecDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'vm', '{"thresholds": {"cpu_percentile_cost": 0.72}}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got1, err := ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got1.CPUPercentileCost, 1e-9)

	_, err = pool.Exec(ctx, `
		UPDATE recommendation_thresholds
		SET thresholds = '{"thresholds": {"cpu_percentile_cost": 0.55}}'::jsonb
		WHERE org_id = $1 AND recommendation_type = 'vm'`, orgID)
	require.NoError(t, err)

	gotCached, err := ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, gotCached.CPUPercentileCost, 1e-9)

	resp := vmSettingsResponseFromConfig(DefaultVMRecConfig())
	resp.Thresholds.CPUPercentileCost = 0.63
	thBytes, err := json.Marshal(resp.Thresholds)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]json.RawMessage{"thresholds": thBytes})
	require.NoError(t, err)
	err = UpdateVMSettings(ctx, pool, orgID, body)
	require.NoError(t, err)

	got2, err := ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.63, got2.CPUPercentileCost, 1e-9, "after PUT cache should be invalidated and refetch from DB")
}

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
