package vm

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// vmClassifySeriesNetwork returns true when sustained network throughput or packet/drop
// patterns indicate a network-optimized (n1) instance type.
func vmClassifySeriesNetwork(digests []model.DailyVMDigest, cfg VMRecConfig) bool {
	if !cfg.EnableNetworkSeries || len(digests) == 0 {
		return false
	}

	sustained := cfg.NetworkSustainedDays
	if sustained < 1 {
		sustained = 7
	}

	throughputThreshold := cfg.NetworkThroughputThresholdBPS
	if throughputThreshold <= 0 {
		throughputThreshold = 62_500_000
	}
	ppsThreshold := cfg.NetworkPPSThreshold
	if ppsThreshold <= 0 {
		ppsThreshold = 100_000
	}
	dropBP := cfg.NetworkDropRatioBP
	if dropBP <= 0 {
		dropBP = 10
	}

	var throughputDays, ppsDropDays int
	for _, d := range digests {
		if d.NetThroughputP95BPS >= throughputThreshold {
			throughputDays++
		}
		if d.NetPPSP95 >= ppsThreshold && d.NetDropRatioMaxBP > dropBP {
			ppsDropDays++
		}
	}
	return throughputDays >= sustained || ppsDropDays >= sustained
}

// vmCPUMemRatioBalanced returns true when vCPU:memory ratio is in the general-purpose band (0.5–2.0).
func vmCPUMemRatioBalanced(vcpu, memGiB int32) bool {
	if vcpu <= 0 || memGiB <= 0 {
		return false
	}
	ratio := float64(vcpu) * 4.0 / float64(memGiB)
	return ratio >= 0.5 && ratio <= 2.0
}
