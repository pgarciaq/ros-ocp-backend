package engine

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// VM disk I/O pattern classification values (stored in vm_recommendations.io_pattern).
const (
	VMIOPatternSequential = "sequential"
	VMIOPatternRandom      = "random"
	VMIOPatternMixed       = "mixed"
	VMIOPatternLowIO       = "low-io"
)

// ClassifyIOPattern derives sequential vs random vs mixed from peak p95 IOPS and throughput.
// Average I/O size = total peak BPS / total peak IOPS (read + write). When peak IOPS is below
// IOMinIOPSForClassification, returns low-io. Empty string when no I/O metrics exist.
func ClassifyIOPattern(digests []model.DailyVMDigest, cfg VMRecConfig) string {
	var peakReadIOPS, peakWriteIOPS, peakReadBPS, peakWriteBPS int64
	for _, d := range digests {
		if d.DiskReadIOPSP95 != nil && *d.DiskReadIOPSP95 > peakReadIOPS {
			peakReadIOPS = *d.DiskReadIOPSP95
		}
		if d.DiskWriteIOPSP95 != nil && *d.DiskWriteIOPSP95 > peakWriteIOPS {
			peakWriteIOPS = *d.DiskWriteIOPSP95
		}
		if d.DiskReadBPS95 != nil && *d.DiskReadBPS95 > peakReadBPS {
			peakReadBPS = *d.DiskReadBPS95
		}
		if d.DiskWriteBPS95 != nil && *d.DiskWriteBPS95 > peakWriteBPS {
			peakWriteBPS = *d.DiskWriteBPS95
		}
	}

	totalIOPS := peakReadIOPS + peakWriteIOPS
	totalBPS := peakReadBPS + peakWriteBPS
	if totalIOPS == 0 || totalBPS == 0 {
		return ""
	}

	minIOPS := cfg.IOMinIOPSForClassification
	if minIOPS < 1 {
		minIOPS = 100
	}
	if totalIOPS < minIOPS {
		return VMIOPatternLowIO
	}

	seqThreshold := cfg.IOSequentialThresholdBytes
	if seqThreshold < 1 {
		seqThreshold = 65536
	}
	randThreshold := cfg.IORandomThresholdBytes
	if randThreshold < 1 {
		randThreshold = 16384
	}

	avgIOSize := totalBPS / totalIOPS
	if avgIOSize >= seqThreshold {
		return VMIOPatternSequential
	}
	if avgIOSize < randThreshold {
		return VMIOPatternRandom
	}
	return VMIOPatternMixed
}
