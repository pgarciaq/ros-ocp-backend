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
