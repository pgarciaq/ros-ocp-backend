package engine_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestVMSettingsCache_HitsOnSecondCall(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-settings-cache-hit"

	config.ResetForTest()
	vm.InitVMRecDefaults(config.GetConfig())
	engine.ClearThresholdSettingsCacheForTest()
	t.Cleanup(engine.ClearThresholdSettingsCacheForTest)

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'vm', '{"thresholds": {"cpu_percentile_cost": 0.72}}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got1, err := vm.ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got1.CPUPercentileCost, 1e-9)

	_, err = pool.Exec(ctx, `
		UPDATE recommendation_thresholds
		SET thresholds = '{"thresholds": {"cpu_percentile_cost": 0.55}}'::jsonb
		WHERE org_id = $1 AND recommendation_type = 'vm'`, orgID)
	require.NoError(t, err)

	got2, err := vm.ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got2.CPUPercentileCost, 1e-9, "second call should return cached value without re-reading DB")
}

func TestVMSettingsCache_InvalidatedOnPUT(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-settings-cache-put"

	config.ResetForTest()
	vm.InitVMRecDefaults(config.GetConfig())
	engine.ClearThresholdSettingsCacheForTest()
	t.Cleanup(engine.ClearThresholdSettingsCacheForTest)

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'vm', '{"thresholds": {"cpu_percentile_cost": 0.72}}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, orgID)
	require.NoError(t, err)

	got1, err := vm.ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, got1.CPUPercentileCost, 1e-9)

	_, err = pool.Exec(ctx, `
		UPDATE recommendation_thresholds
		SET thresholds = '{"thresholds": {"cpu_percentile_cost": 0.55}}'::jsonb
		WHERE org_id = $1 AND recommendation_type = 'vm'`, orgID)
	require.NoError(t, err)

	gotCached, err := vm.ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.72, gotCached.CPUPercentileCost, 1e-9)

	resp := vm.VMSettingsResponseFromConfig(vm.DefaultVMRecConfig())
	resp.Thresholds.CPUPercentileCost = 0.63
	thBytes, err := json.Marshal(resp.Thresholds)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]json.RawMessage{"thresholds": thBytes})
	require.NoError(t, err)
	err = vm.UpdateVMSettings(ctx, pool, orgID, body)
	require.NoError(t, err)

	got2, err := vm.ResolveVMRecConfig(ctx, pool, orgID)
	require.NoError(t, err)
	assert.InDelta(t, 0.63, got2.CPUPercentileCost, 1e-9, "after PUT cache should be invalidated and refetch from DB")
}
