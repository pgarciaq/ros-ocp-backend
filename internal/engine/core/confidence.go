package core

// ComputeConfidence returns the data-coverage confidence level (0.0–1.0).
// It is the ratio of actual data days to the term's full window, clamped to 1.0.
func ComputeConfidence(dataDays, minDataDays, windowDays int) float32 {
	if dataDays <= 0 {
		return 0
	}
	ratio := float32(dataDays) / float32(windowDays)
	if ratio > 1.0 {
		ratio = 1.0
	}
	return ratio
}
