package vm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestVMComputeAdaptiveMargin_LowVariability(t *testing.T) {
	margin := ComputeAdaptiveMarginFromCV([]int64{100, 102, 101, 100, 99}, 0.15, 0.50)
	assert.InDelta(t, 0.15, margin, 0.02)
}

func TestVMComputeAdaptiveMargin_HighVariability(t *testing.T) {
	margin := ComputeAdaptiveMarginFromCV([]int64{100, 300, 50, 400, 80}, 0.15, 0.50)
	assert.InDelta(t, 0.50, margin, 0.02)
}

func TestVMComputeAdaptiveMargin_MidRange(t *testing.T) {
	// CV ~0.29 between low (0.15) and high (0.50) thresholds.
	margin := ComputeAdaptiveMarginFromCV([]int64{100, 200, 100, 200, 150}, 0.15, 0.50)
	assert.Greater(t, margin, 0.15)
	assert.Less(t, margin, 0.50)
}

func TestVMComputeAdaptiveMargin_Disabled(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.CPUAdaptiveMarginEnabled = false
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *Digest) {
		d.CPUUsageP95MC = 100
		d.MemUsageP95KiB = 2 * 1024 * 1024
	})
	digests[1].CPUUsageP95MC = 300
	digests[2].CPUUsageP95MC = 50

	margin := vmResolveCPUMargin(cfg, vmEngineCost, digests, false)
	assert.InDelta(t, cfg.CPUMarginMin, margin, 1e-9)

	marginPerf := vmResolveCPUMargin(cfg, vmEnginePerformance, digests, true)
	assert.InDelta(t, cfg.CPUMarginMax, marginPerf, 1e-9)
}
