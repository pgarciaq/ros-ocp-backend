package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestApplyContainerEnvLocks_UsesConfigWhenEnvPresent(t *testing.T) {
	t.Setenv("ROS_CONTAINER_CPU_COST_PERCENTILE", "0.77")
	config.ResetForTest()
	cfg := config.GetConfig()

	base := DefaultContainerSizingThresholds()
	got := applyContainerEnvLocks(base, cfg)
	assert.InDelta(t, 0.77, got.CPUCostPercentile, 1e-9)
	assert.InDelta(t, base.CPUPerfPercentile, got.CPUPerfPercentile, 1e-9)
}

func TestInitThresholdDefaults_UpdatesPackageDefaults(t *testing.T) {
	t.Setenv("ROS_CONTAINER_IDLE_CPU_THRESHOLD_MC", "15")
	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	assert.Equal(t, int64(15), defaultContainerSizingThresholds.IdleCPUThresholdMC)
}
