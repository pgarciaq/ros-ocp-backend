package engine

const DefaultIdleThresholdMC int64 = 10        // 10 millicores
const DefaultIdleThresholdMemKiB int64 = 10240 // 10 MiB

// TODO: migrate to idle_state=zombie classification; requires updating fleet abandoned_containers count and notification code 8 consumers

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
