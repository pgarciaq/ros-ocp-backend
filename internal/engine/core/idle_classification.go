package core

import (
	"path"
	"strings"
	"time"
)

// IdleState represents the workload activity classification.
type IdleState string

const (
	IdleStateActive IdleState = "active"
	IdleStateIdle   IdleState = "idle"
	IdleStateZombie IdleState = "zombie"
)

// IdleConfig holds thresholds for idle/zombie classification.
// Values come from the 3-tier config model (env > tenant settings > defaults).
type IdleConfig struct {
	Enabled              bool
	ZombieCPUP95MC       int64 // P95 CPU below this = zombie candidate (default 1)
	ZombieCPUPeakMC      int64 // Peak CPU below this confirms zombie (default 10)
	IdleCPUUtilPct       int64 // P95/request % threshold for idle (default 2)
	IdleMemUtilPct       int64 // P95/request % threshold for idle (default 5)
	BurstRatio           int64 // peak/P95 ratio classifying as bursty (default 10)
	MinObservationDays   int   // Days of data required (default 14)
	ExcludeNamespaces    []string
	ExcludeWorkloadTypes []string
}

// DefaultIdleConfig returns compiled defaults.
func DefaultIdleConfig() IdleConfig {
	return IdleConfig{
		Enabled:              true,
		ZombieCPUP95MC:       1,
		ZombieCPUPeakMC:      10,
		IdleCPUUtilPct:       2,
		IdleMemUtilPct:       5,
		BurstRatio:           10,
		MinObservationDays:   14,
		ExcludeNamespaces:    []string{"kube-system", "openshift-*"},
		ExcludeWorkloadTypes: []string{"daemonset"},
	}
}

// IdleResult holds classification output for a single container.
type IdleResult struct {
	State           IdleState
	IdleSince       *time.Time
	DurationDays    int
	PeakCPUMC       int64
	PeakMemoryBytes int64
	WasteCents      int64
}

// ClassifyIdleState determines whether a container is zombie, idle, or active
// based on its digest rows and current resource requests.
func ClassifyIdleState(
	rows []DigestRow,
	currentCPURequestMC int64,
	currentMemRequestKiB int64,
	workloadType string,
	namespace string,
	cfg IdleConfig,
) IdleResult {
	result := IdleResult{State: IdleStateActive}

	if !cfg.Enabled || len(rows) == 0 {
		return result
	}

	if IsExcludedWorkloadType(workloadType, cfg.ExcludeWorkloadTypes) {
		return result
	}
	if IsExcludedNamespace(namespace, cfg.ExcludeNamespaces) {
		return result
	}

	// Early zombie: all samples have zero CPU AND zero memory regardless of
	// observation window length. This subsumes the legacy DetectAbandoned check.
	if AllZeroUsage(rows) {
		result.State = IdleStateZombie
		result.IdleSince = &rows[0].BucketDate
		result.DurationDays = ComputeIdleDuration(result.IdleSince)
		return result
	}

	if len(rows) < cfg.MinObservationDays {
		return result
	}

	cpuP95MC := MaxDailyCPUUsageP95(rows)
	memP95KiB := MaxDailyMemUsageP95(rows)
	peakCPUMC := MaxCPU(rows)
	peakMemBytes := MaxMemBytes(rows)

	result.PeakCPUMC = peakCPUMC
	result.PeakMemoryBytes = peakMemBytes

	_ = memP95KiB // used for idle classification below

	if cpuP95MC > 0 && peakCPUMC > cfg.BurstRatio*cpuP95MC {
		return result
	}

	if cpuP95MC < cfg.ZombieCPUP95MC && peakCPUMC < cfg.ZombieCPUPeakMC {
		result.State = IdleStateZombie
		result.IdleSince = FindIdleSince(rows, func(r DigestRow) bool {
			return r.CPUUsageMaxMC < cfg.ZombieCPUPeakMC
		})
		result.DurationDays = ComputeIdleDuration(result.IdleSince)
		return result
	}

	if currentCPURequestMC > 0 && currentMemRequestKiB > 0 {
		cpuUtilPct := (cpuP95MC * 100) / currentCPURequestMC
		memUtilPct := (memP95KiB * 100) / currentMemRequestKiB
		if cpuUtilPct < cfg.IdleCPUUtilPct && memUtilPct < cfg.IdleMemUtilPct {
			result.State = IdleStateIdle
			result.IdleSince = FindIdleSince(rows, func(r DigestRow) bool {
				if currentCPURequestMC == 0 || currentMemRequestKiB == 0 {
					return false
				}
				return (r.CPUUsageP95MC*100)/currentCPURequestMC < cfg.IdleCPUUtilPct &&
					(r.MemUsageP95KiB*100)/currentMemRequestKiB < cfg.IdleMemUtilPct
			})
			result.DurationDays = ComputeIdleDuration(result.IdleSince)
			return result
		}
	}

	return result
}

// MaxDailyCPUUsageP95 returns the maximum daily CPU P95 across the observation window.
func MaxDailyCPUUsageP95(rows []DigestRow) int64 {
	var max int64
	for _, r := range rows {
		if r.CPUUsageP95MC > max {
			max = r.CPUUsageP95MC
		}
	}
	return max
}

// MaxDailyMemUsageP95 returns the maximum daily memory P95 across the observation window.
func MaxDailyMemUsageP95(rows []DigestRow) int64 {
	var max int64
	for _, r := range rows {
		if r.MemUsageP95KiB > max {
			max = r.MemUsageP95KiB
		}
	}
	return max
}

func MaxCPU(rows []DigestRow) int64 {
	var max int64
	for _, r := range rows {
		if r.CPUUsageMaxMC > max {
			max = r.CPUUsageMaxMC
		}
	}
	return max
}

func MaxMemBytes(rows []DigestRow) int64 {
	var maxKiB int64
	for _, r := range rows {
		if r.MemUsageMaxKiB > maxKiB {
			maxKiB = r.MemUsageMaxKiB
		}
	}
	return maxKiB * 1024
}

func FindIdleSince(rows []DigestRow, predicate func(DigestRow) bool) *time.Time {
	if len(rows) == 0 {
		return nil
	}
	start := len(rows) - 1
	for start >= 0 && predicate(rows[start]) {
		start--
	}
	firstIdle := start + 1
	if firstIdle >= len(rows) {
		return nil
	}
	t := rows[firstIdle].BucketDate
	return &t
}

func IsExcludedWorkloadType(wt string, excludes []string) bool {
	for _, ex := range excludes {
		if strings.EqualFold(wt, ex) {
			return true
		}
	}
	return false
}

func IsExcludedNamespace(ns string, patterns []string) bool {
	for _, pat := range patterns {
		if pat == "" {
			continue
		}
		matched, err := path.Match(pat, ns)
		if err == nil && matched {
			return true
		}
		if ns == pat {
			return true
		}
	}
	return false
}

func ComputeIdleDuration(since *time.Time) int {
	if since == nil {
		return 0
	}
	days := int(time.Since(*since).Truncate(24*time.Hour).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// IsIdleOrZombie returns true when the state is idle or zombie (not active).
func (s IdleState) IsIdleOrZombie() bool {
	return s == IdleStateIdle || s == IdleStateZombie
}

// CategoryFromIdleState maps an IdleState to its category string.
// Returns "" for active (the caller should derive sizing category instead).
func CategoryFromIdleState(state IdleState) string {
	switch state {
	case IdleStateZombie:
		return "zombie"
	case IdleStateIdle:
		return "idle"
	default:
		return ""
	}
}

func IdleStateForWrite(s IdleState) string {
	if s == "" {
		return string(IdleStateActive)
	}
	return string(s)
}

// AllZeroUsage returns true when every digest row has exactly zero CPU and memory
// usage, indicating a completely abandoned/zombie workload.
func AllZeroUsage(rows []DigestRow) bool {
	if len(rows) == 0 {
		return false
	}
	for _, r := range rows {
		if r.CPUUsageMaxMC != 0 || r.MemUsageMaxKiB != 0 {
			return false
		}
	}
	return true
}

// IdleClassificationAuthoritative reports whether ClassifyIdleState applied full
// observation-window rules (not early-return active from disabled/excluded/insufficient data).
func IdleClassificationAuthoritative(cfg IdleConfig, workloadType, namespace string, rows []DigestRow) bool {
	if !cfg.Enabled || len(rows) == 0 {
		return false
	}
	if IsExcludedWorkloadType(workloadType, cfg.ExcludeWorkloadTypes) {
		return false
	}
	if IsExcludedNamespace(namespace, cfg.ExcludeNamespaces) {
		return false
	}
	if AllZeroUsage(rows) {
		return true
	}
	return len(rows) >= cfg.MinObservationDays
}

// SplitCSVList splits a comma-separated string into trimmed non-empty parts.
func SplitCSVList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
