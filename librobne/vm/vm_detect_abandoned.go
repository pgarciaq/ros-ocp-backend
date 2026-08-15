package vm

// DetectVMAbandoned returns true when every digest in digests has zero CPU max and
// zero memory max usage, and there are at least minDays digests (insufficient history
// otherwise).
func DetectVMAbandoned(digests []Digest, minDays int) bool {
	if len(digests) < minDays || minDays < 1 {
		return false
	}
	for _, d := range digests {
		if d.CPUUsageMaxMC > 0 || d.MemUsageMaxKiB > 0 {
			return false
		}
	}
	return true
}
