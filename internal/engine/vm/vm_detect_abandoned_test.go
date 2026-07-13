package vm

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func TestDetectVMAbandoned_AllZero_ReturnsTrue(t *testing.T) {
	rows := []model.DailyVMDigest{
		{CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
		{CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
		{CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
	}
	assert.True(t, DetectVMAbandoned(rows, 3))
}

func TestDetectVMAbandoned_SomeCPU_ReturnsFalse(t *testing.T) {
	rows := []model.DailyVMDigest{
		{CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
		{CPUUsageMaxMC: 1, MemUsageMaxKiB: 0},
	}
	assert.False(t, DetectVMAbandoned(rows, 2))
}

func TestDetectVMAbandoned_SomeMem_ReturnsFalse(t *testing.T) {
	rows := []model.DailyVMDigest{
		{CPUUsageMaxMC: 0, MemUsageMaxKiB: 0},
		{CPUUsageMaxMC: 0, MemUsageMaxKiB: 1024},
	}
	assert.False(t, DetectVMAbandoned(rows, 2))
}

func TestDetectVMAbandoned_Empty_ReturnsFalse(t *testing.T) {
	assert.False(t, DetectVMAbandoned(nil, 3))
}
