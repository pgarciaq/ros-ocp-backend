package gpu

import (
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gpuIdleConfig() GPUIdleConfig {
	return GPUIdleConfig{
		Enabled:            true,
		IdleSMActiveBP:     500,
		IdleDRAMActiveBP:   500,
		ZombieSMActiveBP:   100,
		ZombieDRAMActiveBP: 100,
		MinObservationDays: 7,
	}
}

func gpuDigestDay(day int, smAvg, dramAvg int32) GPUDigestRow {
	return GPUDigestRow{
		IntervalStart: time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC),
		SMActiveAvg:   smAvg,
		DRAMActiveAvg: dramAvg,
	}
}

func gpuObservationRows(n int, smAvg, dramAvg int32) []GPUDigestRow {
	rows := make([]GPUDigestRow, n)
	for i := range rows {
		rows[i] = gpuDigestDay(i+1, smAvg, dramAvg)
	}
	return rows
}

func TestClassifyGPUIdleState_Zombie(t *testing.T) {
	cfg := gpuIdleConfig()
	result := ClassifyGPUIdleState(50, 80, 7, cfg)
	assert.Equal(t, types.IdleStateZombie, result.State)
}

func TestClassifyGPUIdleState_Idle(t *testing.T) {
	cfg := gpuIdleConfig()
	result := ClassifyGPUIdleState(200, 300, 7, cfg)
	assert.Equal(t, types.IdleStateIdle, result.State)
}

func TestClassifyGPUIdleState_Active(t *testing.T) {
	cfg := gpuIdleConfig()
	result := ClassifyGPUIdleState(600, 100, 7, cfg)
	assert.Equal(t, types.IdleStateActive, result.State)

	result = ClassifyGPUIdleState(200, 600, 7, cfg)
	assert.Equal(t, types.IdleStateActive, result.State)
}

func TestClassifyGPUIdleState_Disabled(t *testing.T) {
	cfg := gpuIdleConfig()
	cfg.Enabled = false
	result := ClassifyGPUIdleState(0, 0, 7, cfg)
	assert.Equal(t, types.IdleStateActive, result.State)
}

func TestClassifyGPUIdleState_InsufficientObservationDays(t *testing.T) {
	cfg := gpuIdleConfig()
	result := ClassifyGPUIdleState(0, 0, 5, cfg)
	assert.Equal(t, types.IdleStateActive, result.State)
}

func TestClassifyGPUIdleState_ThresholdEdgeIdle(t *testing.T) {
	cfg := gpuIdleConfig()
	// At zombie boundary (100bp): not zombie (need strictly less)
	result := ClassifyGPUIdleState(100, 100, 7, cfg)
	assert.Equal(t, types.IdleStateIdle, result.State)

	// At idle boundary (500bp): not idle (need strictly less)
	result = ClassifyGPUIdleState(500, 500, 7, cfg)
	assert.Equal(t, types.IdleStateActive, result.State)
}

func TestClassifyGPUIdleFromDigests_SetsIdleSince(t *testing.T) {
	cfg := gpuIdleConfig()
	rows := gpuObservationRows(7, 50, 50)
	result := ClassifyGPUIdleFromDigests(rows, cfg)
	assert.Equal(t, types.IdleStateZombie, result.State)
	require.NotNil(t, result.IdleSince)
	assert.Equal(t, 1, result.IdleSince.Day())
	assert.Greater(t, result.DurationDays, 0)
}

func TestClassifyGPUIdleFromDigests_MaxDailyP95TreatsEarlySpikeAsActive(t *testing.T) {
	cfg := gpuIdleConfig()
	rows := make([]GPUDigestRow, 20)
	for i := range rows {
		sm, dram := int32(50), int32(50)
		if i == 0 {
			sm, dram = 800, 800
		}
		rows[i] = gpuDigestDay(i+1, sm, dram)
	}
	result := ClassifyGPUIdleFromDigests(rows, cfg)
	assert.Equal(t, types.IdleStateActive, result.State, "max daily P95 includes early spike; conservative for idle detection")
	assert.Nil(t, result.IdleSince)
}

func TestRecommendGPU_SetsGPUIdleState(t *testing.T) {
	digests := gpuObservationRows(7, 50, 50)
	idleCfg := GPUIdleConfig{
		Enabled:            true,
		IdleSMActiveBP:     500,
		IdleDRAMActiveBP:   500,
		ZombieSMActiveBP:   100,
		ZombieDRAMActiveBP: 100,
		MinObservationDays: 7,
	}
	rec := RecommendGPUWithSettings(digests, defaultGPUThresholdSettings, idleCfg)
	require.NotNil(t, rec)
	assert.Equal(t, types.IdleStateZombie, rec.GPUIdleState)
}
