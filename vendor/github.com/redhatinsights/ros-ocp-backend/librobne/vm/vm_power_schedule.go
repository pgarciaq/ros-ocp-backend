package vm

import (
	"math"
	"strings"
)

// NotifVMPowerOffSchedule recommends scheduling power-off during historically idle periods.
const NotifVMPowerOffSchedule int16 = 64

// DetectPowerOffCandidate returns whether a VM is mostly idle on observed days but still
// shows occasional activity (periodically idle, not abandoned). savingsMultiplier is the
// fraction of observed days that were idle (e.g. 0.7 for 70%).
func DetectPowerOffCandidate(digests []Digest, cfg VMRecConfig) (bool, *float64) {
	if !cfg.EnablePowerSchedule {
		return false, nil
	}
	minDays := int(cfg.PowerOffMinIdleDays)
	if minDays < 1 {
		minDays = 14
	}
	if len(digests) < minDays {
		return false, nil
	}

	guestOS := strings.TrimSpace(latestVMDigest(digests).GuestOS)
	isWindows := guestOS != "" && vmIsWindows(guestOS)

	var idleDays, activeDays int
	for _, d := range digests {
		if isDigestIdle(d, cfg, isWindows) {
			idleDays++
		} else {
			activeDays++
		}
	}
	if activeDays == 0 {
		return false, nil
	}

	idleRatio := float64(idleDays) / float64(len(digests))
	threshold := cfg.PowerOffIdleRatioThreshold
	if threshold <= 0 {
		threshold = 0.7
	}
	if idleRatio >= threshold {
		return true, &idleRatio
	}
	return false, nil
}

func isDigestIdle(d Digest, cfg VMRecConfig, isWindows bool) bool {
	cpuThreshold := cfg.IdleCPUMC
	memKiB := cfg.IdleMemoryMiB * 1024
	if isWindows {
		cpuThreshold = cfg.IdleCPUMCWindows
		memKiB = cfg.IdleMemoryMiBWindows * 1024
	}
	return d.CPUUsageP95MC < cpuThreshold && d.MemUsageP95KiB < memKiB
}

// PowerOffIdleRatioBasisPoints converts an idle-day ratio to basis points (0–10000).
func PowerOffIdleRatioBasisPoints(idleRatio float64) int32 {
	if idleRatio <= 0 {
		return 0
	}
	if idleRatio >= 1 {
		return 10000
	}
	return int32(math.Round(idleRatio * 10000))
}

// PowerOffIdlePercentFromBasisPoints converts stored basis points to an integer percent for API display.
func PowerOffIdlePercentFromBasisPoints(bp int32) int32 {
	if bp <= 0 {
		return 0
	}
	if bp >= 10000 {
		return 100
	}
	return int32(math.Round(float64(bp) / 100))
}
