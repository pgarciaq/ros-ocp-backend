package types

// SelectCPUUsagePercentile returns the pre-computed CPU usage percentile
// column from a DigestRow matching the requested percentile level.
// Supported: 0.50, 0.60, 0.95, 0.98, 0.99, 1.0 (max).
// Unsupported values fall back to the nearest lower available percentile.
//
// The percentile-to-column mapping lives in CPUPercentileColumn so the
// allocation-free Column walk and this closure form can never disagree; this
// function is the closure-shaped view of that single mapping.
func SelectCPUUsagePercentile(row DigestRow, pct float64) int64 {
	return CPUPercentileColumn(pct).Value(&row)
}

// SelectMemUsagePercentile returns the pre-computed memory usage percentile
// column from a DigestRow matching the requested percentile level.
// Supported: 0.50, 0.60, 0.95, 0.98, 0.99, 1.0 (max).
// Unsupported values fall back to the nearest lower available percentile.
//
// See SelectCPUUsagePercentile for why the mapping is not duplicated here.
func SelectMemUsagePercentile(row DigestRow, pct float64) int64 {
	return MemPercentileColumn(pct).Value(&row)
}
