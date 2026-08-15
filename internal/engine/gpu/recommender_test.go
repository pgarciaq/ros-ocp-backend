package gpu

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestGPUThresholdsFromConfig(t *testing.T) {
	cfg := &config.Config{
		GPUIdleThreshold:                0.05,
		GPUUnderutilizedSMThreshold:     0.99,
		GPUUnderutilizedTensorThreshold: 0.20,
		GPUMemBoundDRAMThreshold:        0.70,
		GPUMemBoundTensorThreshold:      0.18,
		GPUFBHeadroomFactor:             1.30,
	}
	th := GPUThresholdsFromConfig(cfg)
	assert.InDelta(t, 0.05, th.IdleThreshold, 1e-9)
	assert.InDelta(t, 0.99, th.UnderutilizedSM, 1e-9)
	assert.InDelta(t, 0.20, th.UnderutilizedTensor, 1e-9)
	assert.InDelta(t, 0.70, th.MemBoundDRAM, 1e-9)
	assert.InDelta(t, 0.18, th.MemBoundTensor, 1e-9)
	assert.InDelta(t, 1.30, th.FBHeadroomFactor, 1e-9)
}

func TestGPUThresholdsFromConfig_Nil(t *testing.T) {
	th := GPUThresholdsFromConfig(nil)
	defaults := DefaultGPUThresholds()
	assert.Equal(t, defaults, th)
}

func TestRecommendGPU_NoGPUData(t *testing.T) {
	assert.Nil(t, RecommendGPU(nil))
}
