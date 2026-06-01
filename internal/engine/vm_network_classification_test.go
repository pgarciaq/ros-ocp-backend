package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func TestVMClassifySeriesNetwork_ThroughputSustained(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.NetworkSustainedDays = 3
	cfg.NetworkThroughputThresholdBPS = 50_000_000

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := make([]model.DailyVMDigest, 5)
	for i := range digests {
		digests[i] = model.DailyVMDigest{
			BucketDate:            base.AddDate(0, 0, i),
			NetThroughputP95BPS:   60_000_000,
			NetPPSP95:             1000,
			NetDropRatioMaxBP:     0,
		}
	}
	assert.True(t, vmClassifySeriesNetwork(digests, cfg))
}

func TestVMClassifySeriesNetwork_PPSAndDropsSustained(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.NetworkSustainedDays = 3
	cfg.NetworkPPSThreshold = 100_000
	cfg.NetworkDropRatioBP = 10

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := make([]model.DailyVMDigest, 4)
	for i := range digests {
		digests[i] = model.DailyVMDigest{
			BucketDate:            base.AddDate(0, 0, i),
			NetThroughputP95BPS:   1_000_000,
			NetPPSP95:             150_000,
			NetDropRatioMaxBP:     15,
		}
	}
	assert.True(t, vmClassifySeriesNetwork(digests, cfg))
}

func TestVMClassifySeriesNetwork_Disabled(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.EnableNetworkSeries = false
	digests := []model.DailyVMDigest{{NetThroughputP95BPS: 100_000_000}}
	assert.False(t, vmClassifySeriesNetwork(digests, cfg))
}

func TestVMClassifySeries_NetworkOptimizedWhenBalanced(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.NetworkSustainedDays = 1
	cfg.NetworkThroughputThresholdBPS = 1

	digests := []model.DailyVMDigest{{
		NetThroughputP95BPS: 62_500_000,
	}}
	assert.Equal(t, vmSeriesNetworkOptimized, vmClassifySeries(digests, 4, 16, false, cfg))
}

func TestVMCPUMemRatioBalanced(t *testing.T) {
	assert.True(t, vmCPUMemRatioBalanced(4, 16))
	assert.False(t, vmCPUMemRatioBalanced(8, 2))
}
