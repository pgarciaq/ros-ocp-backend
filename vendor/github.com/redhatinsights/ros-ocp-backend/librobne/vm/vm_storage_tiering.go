package vm

import (
	"fmt"
)

// VM storage tiering notification codes (simplified pattern-based hints).
const (
	NotifVMStorageTierCold       int16 = 67
	NotifVMStorageTierIOPS       int16 = 68
	NotifVMStorageTierThroughput int16 = 69
)

// EvaluateStorageTiering emits storage tier suggestions from multi-day I/O patterns.
// Full tiering with StorageClass awareness and savings is future work.
func EvaluateStorageTiering(digests []Digest, cfg VMRecConfig) []VMNotification {
	if !cfg.StorageTieringEnabled || len(digests) < cfg.StorageTieringMinDays {
		return nil
	}

	var (
		lowIOCount                    int
		randomHighIOPSCount           int
		sequentialHighThroughputCount int
	)

	iopsThreshold := cfg.StorageTieringHighIOPSThreshold
	if iopsThreshold < 1 {
		iopsThreshold = 5000
	}
	throughputThreshold := cfg.StorageTieringHighThroughputBPS
	if throughputThreshold < 1 {
		throughputThreshold = 104857600
	}

	for _, d := range digests {
		ioPattern := classifyDigestIOPattern(d, cfg)
		switch ioPattern {
		case VMIOPatternLowIO:
			lowIOCount++
		case VMIOPatternRandom:
			if digestDiskIOPS(d) > iopsThreshold {
				randomHighIOPSCount++
			}
		case VMIOPatternSequential:
			if digestDiskBPS(d) > throughputThreshold {
				sequentialHighThroughputCount++
			}
		}
	}

	coldMin := cfg.StorageTieringColdMinDays
	if coldMin < 1 {
		coldMin = 14
	}
	iopsMin := cfg.StorageTieringIOPSMinDays
	if iopsMin < 1 {
		iopsMin = 7
	}
	throughputMin := cfg.StorageTieringThroughputMinDays
	if throughputMin < 1 {
		throughputMin = 7
	}

	var notifications []VMNotification
	window := len(digests)

	if lowIOCount >= coldMin {
		notifications = append(notifications, VMNotification{
			Code: NotifVMStorageTierCold,
			Type: vmNotifTypeInfo,
			Message: fmt.Sprintf(
				"VM disk has minimal I/O activity (%d of %d days) — consider migrating to a lower-cost storage tier",
				lowIOCount, window,
			),
		})
	}
	if randomHighIOPSCount >= iopsMin {
		notifications = append(notifications, VMNotification{
			Code: NotifVMStorageTierIOPS,
			Type: vmNotifTypeInfo,
			Message: fmt.Sprintf(
				"VM disk shows sustained random I/O pattern (%d of %d days with high IOPS) — IOPS-optimized storage recommended",
				randomHighIOPSCount, window,
			),
		})
	}
	if sequentialHighThroughputCount >= throughputMin {
		notifications = append(notifications, VMNotification{
			Code: NotifVMStorageTierThroughput,
			Type: vmNotifTypeInfo,
			Message: fmt.Sprintf(
				"VM disk shows sustained sequential I/O pattern (%d of %d days with high throughput) — throughput-optimized storage recommended",
				sequentialHighThroughputCount, window,
			),
		})
	}

	return notifications
}

// classifyDigestIOPattern classifies a single day's disk I/O using the same rules as ClassifyIOPattern.
func classifyDigestIOPattern(d Digest, cfg VMRecConfig) string {
	return ClassifyIOPattern([]Digest{d}, cfg)
}

func digestDiskIOPS(d Digest) int64 {
	var total int64
	if d.DiskReadIOPSP95 != nil {
		total += *d.DiskReadIOPSP95
	}
	if d.DiskWriteIOPSP95 != nil {
		total += *d.DiskWriteIOPSP95
	}
	return total
}

func digestDiskBPS(d Digest) int64 {
	var total int64
	if d.DiskReadBPS95 != nil {
		total += *d.DiskReadBPS95
	}
	if d.DiskWriteBPS95 != nil {
		total += *d.DiskWriteBPS95
	}
	return total
}
