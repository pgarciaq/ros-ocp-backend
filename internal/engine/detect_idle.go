package engine

const DefaultIdleThresholdMC int64 = 10        // 10 millicores
const DefaultIdleThresholdMemKiB int64 = 10240 // 10 MiB

// DetectIdle returns true if the maximum CPU usage AND memory usage across all
// digest rows are both strictly below their respective thresholds.
// thresholdMC is in millicores; thresholdMemKiB is in KiB.
// Returns false for empty input.
func DetectIdle(rows []DigestRow, thresholdMC, thresholdMemKiB int64) bool {
	if len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		if row.CPUUsageMaxMC >= thresholdMC || row.MemUsageMaxKiB >= thresholdMemKiB {
			return false
		}
	}
	return true
}

// DetectAbandoned returns true if ALL rows have exactly zero CPU usage AND
// zero memory usage, indicating a completely abandoned workload.
// Returns false for empty input.
func DetectAbandoned(rows []DigestRow) bool {
	if len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		if row.CPUUsageMaxMC != 0 || row.MemUsageMaxKiB != 0 {
			return false
		}
	}
	return true
}
