package vm

import (
	"math"
)

// ComputeAdaptiveMarginFromCV maps CPU usage variability (coefficient of variation of
// daily P95 millicores) to a safety margin between minMargin and maxMargin.
// CV = stddev(daily P95) / mean(daily P95). Low CV (< 0.15) → minMargin; high CV (> 0.50) → maxMargin.
func ComputeAdaptiveMarginFromCV(dailyP95MC []int64, minMargin, maxMargin float64) float64 {
	if len(dailyP95MC) == 0 {
		return minMargin
	}
	var sum float64
	for _, v := range dailyP95MC {
		sum += float64(v)
	}
	mean := sum / float64(len(dailyP95MC))
	if mean <= 0 {
		return minMargin
	}
	var sqDiff float64
	for _, v := range dailyP95MC {
		d := float64(v) - mean
		sqDiff += d * d
	}
	stddev := math.Sqrt(sqDiff / float64(len(dailyP95MC)))
	cv := stddev / mean

	const cvLow = 0.15
	const cvHigh = 0.50
	if cv <= cvLow {
		return minMargin
	}
	if cv >= cvHigh {
		return maxMargin
	}
	frac := (cv - cvLow) / (cvHigh - cvLow)
	return minMargin + frac*(maxMargin-minMargin)
}

// vmResolveCPUMargin returns the CPU safety margin for the given engine and digest window.
func vmResolveCPUMargin(cfg VMRecConfig, engine string, days []Digest, useP99 bool) float64 {
	if engine == vmEnginePerformance {
		return cfg.CPUMarginMax
	}
	if !cfg.CPUAdaptiveMarginEnabled {
		return cfg.CPUMarginMin
	}
	return ComputeAdaptiveMarginFromCV(vmDailyCPUP95Values(days, useP99), cfg.CPUMarginMin, cfg.CPUMarginMax)
}

// vmDailyCPUP95Values collects per-day P95 CPU usage (millicores) for variability analysis.
func vmDailyCPUP95Values(days []Digest, useP99 bool) []int64 {
	out := make([]int64, 0, len(days))
	for _, d := range days {
		v := d.CPUUsageP95MC
		if useP99 {
			v = d.CPUUsageP99MC
		}
		out = append(out, v)
	}
	return out
}
