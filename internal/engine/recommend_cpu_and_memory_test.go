package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRecommendCPUAndMemory_MatchesSeparateCalls(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now.AddDate(0, 0, -3), CPUUsageP60MC: 100, CPUUsageP95MC: 150, CPUUsageP50MC: 80, CPUUsageMeanMC: 90, CPUUsageP98MC: 180, CPUUsageMaxMC: 200, MemUsageP95KiB: 1024, MemUsageP50KiB: 512, MemUsageMeanKiB: 600, MemUsageMaxKiB: 2048},
		{BucketDate: now.AddDate(0, 0, -2), CPUUsageP60MC: 120, CPUUsageP95MC: 170, CPUUsageP50MC: 90, CPUUsageMeanMC: 100, CPUUsageP98MC: 190, CPUUsageMaxMC: 210, MemUsageP95KiB: 1100, MemUsageP50KiB: 550, MemUsageMeanKiB: 650, MemUsageMaxKiB: 2200},
		{BucketDate: now.AddDate(0, 0, -1), CPUUsageP60MC: 140, CPUUsageP95MC: 190, CPUUsageP50MC: 100, CPUUsageMeanMC: 110, CPUUsageP98MC: 200, CPUUsageMaxMC: 220, MemUsageP95KiB: 1200, MemUsageP50KiB: 600, MemUsageMeanKiB: 700, MemUsageMaxKiB: 2400},
	}
	cpuCfg := DefaultCPUConfig(now, 72)
	memCfg := DefaultMemoryConfig(now, 72)
	memCfg.OOMCountSum = 2

	fusedCPU, fusedMem, _ := RecommendCPUAndMemory(rows, cpuCfg, memCfg)
	separateCPU := RecommendCPU(rows, cpuCfg)
	separateMem := RecommendMemory(rows, memCfg)

	assert.Equal(t, separateCPU, fusedCPU)
	assert.Equal(t, separateMem, fusedMem)
}

func TestRecommendCPUAndMemory_EmptyRows(t *testing.T) {
	now := time.Now().UTC()
	cpuRec, memRec, _ := RecommendCPUAndMemory(nil, DefaultCPUConfig(now, 72), DefaultMemoryConfig(now, 72))
	assert.Equal(t, CPURec{}, cpuRec)
	assert.Equal(t, MemoryRec{}, memRec)
}

func TestRecommendCPUAndMemory_MemFloor_AppliedWhenBelowThreshold(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, MemUsageP95KiB: 10, MemUsageP50KiB: 5, MemUsageMeanKiB: 7, MemUsageMaxKiB: 15,
			CPUUsageP60MC: 100, CPUUsageP95MC: 150, CPUUsageP50MC: 80, CPUUsageMeanMC: 90, CPUUsageP98MC: 180, CPUUsageMaxMC: 200},
	}
	cpuCfg := DefaultCPUConfig(now, 0)
	memCfg := DefaultMemoryConfig(now, 0)

	_, memRec, expl := RecommendCPUAndMemory(rows, cpuCfg, memCfg)

	assert.GreaterOrEqual(t, memRec.CostRequestKiB, int64(4096), "cost request must be at least the 4 MiB floor")
	assert.GreaterOrEqual(t, memRec.PerfRequestKiB, int64(4096), "perf request must be at least the 4 MiB floor")
	assert.GreaterOrEqual(t, memRec.CostLimitKiB, memRec.CostRequestKiB, "limit must be >= request")
	assert.True(t, expl.MemFloorApplied, "MemFloorApplied explanation factor should be true")
}

func TestRecommendCPUAndMemory_MemFloor_NotAppliedWhenAboveThreshold(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, MemUsageP95KiB: 100_000, MemUsageP50KiB: 80_000, MemUsageMeanKiB: 90_000, MemUsageMaxKiB: 120_000,
			CPUUsageP60MC: 500, CPUUsageP95MC: 800, CPUUsageP50MC: 400, CPUUsageMeanMC: 450, CPUUsageP98MC: 900, CPUUsageMaxMC: 1000},
	}
	cpuCfg := DefaultCPUConfig(now, 0)
	memCfg := DefaultMemoryConfig(now, 0)

	_, memRec, expl := RecommendCPUAndMemory(rows, cpuCfg, memCfg)

	assert.Greater(t, memRec.CostRequestKiB, int64(4096), "high-usage container should be well above the floor")
	assert.False(t, expl.MemFloorApplied, "MemFloorApplied should be false when value exceeds floor")
}

func TestRecommendCPUAndMemory_MemFloor_ZeroUsageGetsFloor(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, MemUsageP95KiB: 0, MemUsageP50KiB: 0, MemUsageMeanKiB: 0, MemUsageMaxKiB: 0,
			CPUUsageP60MC: 50, CPUUsageP95MC: 80, CPUUsageP50MC: 40, CPUUsageMeanMC: 45, CPUUsageP98MC: 90, CPUUsageMaxMC: 100},
	}
	cpuCfg := DefaultCPUConfig(now, 0)
	memCfg := DefaultMemoryConfig(now, 0)

	_, memRec, expl := RecommendCPUAndMemory(rows, cpuCfg, memCfg)

	assert.Equal(t, int64(4096), memRec.CostRequestKiB, "zero usage should get exactly the floor value")
	assert.Equal(t, int64(4096), memRec.PerfRequestKiB, "zero usage perf should get exactly the floor value")
	assert.True(t, expl.MemFloorApplied, "MemFloorApplied should be true for zero usage")
}

func TestRecommendCPUAndMemory_MemFloor_CustomFloorValue(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, MemUsageP95KiB: 10, MemUsageP50KiB: 5, MemUsageMeanKiB: 7, MemUsageMaxKiB: 15,
			CPUUsageP60MC: 100, CPUUsageP95MC: 150, CPUUsageP50MC: 80, CPUUsageMeanMC: 90, CPUUsageP98MC: 180, CPUUsageMaxMC: 200},
	}
	cpuCfg := DefaultCPUConfig(now, 0)
	memCfg := DefaultMemoryConfig(now, 0)
	memCfg.FloorKiB = 8192 // 8 MiB custom floor

	_, memRec, expl := RecommendCPUAndMemory(rows, cpuCfg, memCfg)

	assert.GreaterOrEqual(t, memRec.CostRequestKiB, int64(8192), "custom floor should be respected")
	assert.True(t, expl.MemFloorApplied)
}

func TestRecommendMemory_FloorApplied(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, MemUsageP95KiB: 10, MemUsageP50KiB: 5, MemUsageMeanKiB: 7, MemUsageMaxKiB: 15},
	}
	cfg := DefaultMemoryConfig(now, 0)

	rec := RecommendMemory(rows, cfg)

	assert.GreaterOrEqual(t, rec.CostRequestKiB, int64(4096), "separate RecommendMemory path should also apply floor")
	assert.GreaterOrEqual(t, rec.PerfRequestKiB, int64(4096))
}

func TestRecommendMemory_FloorNotAppliedAboveThreshold(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, MemUsageP95KiB: 500_000, MemUsageP50KiB: 400_000, MemUsageMeanKiB: 450_000, MemUsageMaxKiB: 600_000},
	}
	cfg := DefaultMemoryConfig(now, 0)

	rec := RecommendMemory(rows, cfg)

	assert.Greater(t, rec.CostRequestKiB, int64(4096), "high-usage should not be clamped to floor")
}

func TestRecommendCPUAndMemory_MemFloor_SavingsUseFloorNotZero(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, MemUsageP95KiB: 0, MemUsageP50KiB: 0, MemUsageMeanKiB: 0, MemUsageMaxKiB: 0,
			CPUUsageP60MC: 50, CPUUsageP95MC: 80, CPUUsageP50MC: 40, CPUUsageMeanMC: 45, CPUUsageP98MC: 90, CPUUsageMaxMC: 100},
	}
	cpuCfg := DefaultCPUConfig(now, 0)
	memCfg := DefaultMemoryConfig(now, 0)

	_, memRec, _ := RecommendCPUAndMemory(rows, cpuCfg, memCfg)

	// The recommended memory request should be the floor, not 0.
	// This ensures savings are computed against 4 MiB, not 0.
	assert.Equal(t, int64(4096), memRec.CostRequestKiB,
		"with zero usage, recommended memory should be the floor value for savings computation")
}

func TestDefaultContainerSizingThresholds_MemFloorKiB(t *testing.T) {
	th := DefaultContainerSizingThresholds()
	assert.Equal(t, int64(4096), th.MemFloorKiB, "default memory floor should be 4096 KiB (4 MiB)")
}

func TestDefaultMemoryConfig_FloorKiB(t *testing.T) {
	now := time.Now().UTC()
	cfg := DefaultMemoryConfig(now, 72)
	assert.Equal(t, int64(4096), cfg.FloorKiB, "default MemoryConfig should carry 4096 KiB floor")
}
