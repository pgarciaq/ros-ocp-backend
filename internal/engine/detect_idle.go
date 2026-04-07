package engine

// DetectIdle returns true if the maximum CPU usage across all digest rows
// is strictly below the threshold (in millicores).
// Returns false for empty input.
func DetectIdle(rows []DigestRow, thresholdMC int64) bool {
	if len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		if row.CPUUsageMaxMC >= thresholdMC {
			return false
		}
	}
	return true
}
