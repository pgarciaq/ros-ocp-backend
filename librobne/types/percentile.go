package types

// SelectCPUUsagePercentile returns the pre-computed CPU usage percentile
// column from a DigestRow matching the requested percentile level.
// Supported: 0.50, 0.60, 0.95, 0.98, 0.99, 1.0 (max).
// Unsupported values fall back to the nearest lower available percentile.
func SelectCPUUsagePercentile(row DigestRow, pct float64) int64 {
	switch {
	case pct >= 1.0:
		return row.CPUUsageMaxMC
	case pct >= 0.99:
		return row.CPUUsageP99MC
	case pct >= 0.98:
		return row.CPUUsageP98MC
	case pct >= 0.95:
		return row.CPUUsageP95MC
	case pct >= 0.60:
		return row.CPUUsageP60MC
	default:
		return row.CPUUsageP50MC
	}
}

// SelectMemUsagePercentile returns the pre-computed memory usage percentile
// column from a DigestRow matching the requested percentile level.
// Supported: 0.50, 0.60, 0.95, 0.98, 0.99, 1.0 (max).
// Unsupported values fall back to the nearest lower available percentile.
func SelectMemUsagePercentile(row DigestRow, pct float64) int64 {
	switch {
	case pct >= 1.0:
		return row.MemUsageMaxKiB
	case pct >= 0.99:
		return row.MemUsageP99KiB
	case pct >= 0.98:
		return row.MemUsageP98KiB
	case pct >= 0.95:
		return row.MemUsageP95KiB
	case pct >= 0.60:
		return row.MemUsageP60KiB
	default:
		return row.MemUsageP50KiB
	}
}
