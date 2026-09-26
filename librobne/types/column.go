package types

// Column identifies one numeric field of DigestRow.
//
// A Column is the allocation-free alternative to a `func(DigestRow) int64`
// extractor. Two costs make the closure form expensive on the recommendation hot
// path (#602):
//
//  1. A closure that captures a runtime value (a percentile) cannot be allocated
//     statically, so building the extractor list costs one heap allocation per
//     capturing closure per call.
//  2. The extractor signature takes DigestRow **by value**, and DigestRow is ~312
//     bytes. Every column of every row therefore copies the whole struct onto the
//     stack: a 30-row, 10-column pass copies ~94 KB per call.
//
// A Column is a uint8, so a list of them is plain data that stays on the stack,
// and Value takes a *DigestRow so it reads the field in place from the row the
// walk already holds.
//
// The trade-off is that a Column can only name a column that exists, so dynamic
// selectors must be resolved before the walk. That is what CPUPercentileColumn
// and MemPercentileColumn do: they resolve a configured percentile to the
// concrete column the percentile chain would have selected, so there is still
// exactly one percentile-to-column mapping in this package.
type Column uint8

const (
	// ColNone is the zero value and means "no column". A zero Column in an
	// extractor list would silently read garbage, so callers must never append
	// it; using it as the zero value lets a zero opts struct mean "don't track".
	ColNone Column = iota

	ColCPUUsageP50MC
	ColCPUUsageP60MC
	ColCPUUsageP95MC
	ColCPUUsageP98MC
	ColCPUUsageP99MC
	ColCPUUsageMaxMC
	ColCPUUsageMeanMC

	ColMemUsageP50KiB
	ColMemUsageP60KiB
	ColMemUsageP95KiB
	ColMemUsageP98KiB
	ColMemUsageP99KiB
	ColMemUsageMaxKiB
	ColMemUsageMeanKiB
)

// columnCount is the number of valid Column values, ColNone included. Tests use
// it to assert the Value switch covers every constant.
const columnCount = int(ColMemUsageMeanKiB) + 1

// Value returns the named column's value. The receiver is a pointer so the
// 312-byte DigestRow is read in place rather than copied per column.
//
// A ColNone (or any out-of-range) value returns 0 rather than panicking: the
// percentile resolvers below can never produce it, and a recommendation reading
// 0 degrades to the configured floor instead of crashing a processor batch.
func (c Column) Value(r *DigestRow) int64 {
	switch c {
	case ColCPUUsageP50MC:
		return r.CPUUsageP50MC
	case ColCPUUsageP60MC:
		return r.CPUUsageP60MC
	case ColCPUUsageP95MC:
		return r.CPUUsageP95MC
	case ColCPUUsageP98MC:
		return r.CPUUsageP98MC
	case ColCPUUsageP99MC:
		return r.CPUUsageP99MC
	case ColCPUUsageMaxMC:
		return r.CPUUsageMaxMC
	case ColCPUUsageMeanMC:
		return r.CPUUsageMeanMC
	case ColMemUsageP50KiB:
		return r.MemUsageP50KiB
	case ColMemUsageP60KiB:
		return r.MemUsageP60KiB
	case ColMemUsageP95KiB:
		return r.MemUsageP95KiB
	case ColMemUsageP98KiB:
		return r.MemUsageP98KiB
	case ColMemUsageP99KiB:
		return r.MemUsageP99KiB
	case ColMemUsageMaxKiB:
		return r.MemUsageMaxKiB
	case ColMemUsageMeanKiB:
		return r.MemUsageMeanKiB
	default:
		return 0
	}
}

// String makes a Column readable in test failures and profiles.
func (c Column) String() string {
	switch c {
	case ColNone:
		return "none"
	case ColCPUUsageP50MC:
		return "cpu_p50"
	case ColCPUUsageP60MC:
		return "cpu_p60"
	case ColCPUUsageP95MC:
		return "cpu_p95"
	case ColCPUUsageP98MC:
		return "cpu_p98"
	case ColCPUUsageP99MC:
		return "cpu_p99"
	case ColCPUUsageMaxMC:
		return "cpu_max"
	case ColCPUUsageMeanMC:
		return "cpu_mean"
	case ColMemUsageP50KiB:
		return "mem_p50"
	case ColMemUsageP60KiB:
		return "mem_p60"
	case ColMemUsageP95KiB:
		return "mem_p95"
	case ColMemUsageP98KiB:
		return "mem_p98"
	case ColMemUsageP99KiB:
		return "mem_p99"
	case ColMemUsageMaxKiB:
		return "mem_max"
	case ColMemUsageMeanKiB:
		return "mem_mean"
	default:
		return "invalid"
	}
}

// CPUPercentileColumn resolves a configured CPU percentile to the column the
// percentile chain selects. Supported: 0.50, 0.60, 0.95, 0.98, 0.99, 1.0 (max).
// Unsupported values fall back to the nearest lower available percentile.
//
// The comparison order is load-bearing and must not be reordered: it is the same
// descending if-chain the original selector used, so every input — including
// values between the documented percentiles, negatives, and NaN (every
// comparison false, so the default arm wins) — resolves identically.
func CPUPercentileColumn(pct float64) Column {
	switch {
	case pct >= 1.0:
		return ColCPUUsageMaxMC
	case pct >= 0.99:
		return ColCPUUsageP99MC
	case pct >= 0.98:
		return ColCPUUsageP98MC
	case pct >= 0.95:
		return ColCPUUsageP95MC
	case pct >= 0.60:
		return ColCPUUsageP60MC
	default:
		return ColCPUUsageP50MC
	}
}

// MemPercentileColumn resolves a configured memory percentile to its column.
// Same chain and same ordering guarantees as CPUPercentileColumn.
func MemPercentileColumn(pct float64) Column {
	switch {
	case pct >= 1.0:
		return ColMemUsageMaxKiB
	case pct >= 0.99:
		return ColMemUsageP99KiB
	case pct >= 0.98:
		return ColMemUsageP98KiB
	case pct >= 0.95:
		return ColMemUsageP95KiB
	case pct >= 0.60:
		return ColMemUsageP60KiB
	default:
		return ColMemUsageP50KiB
	}
}
