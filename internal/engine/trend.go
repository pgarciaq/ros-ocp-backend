package engine

// ComputeTrendSlope computes a simple least-squares linear regression slope
// over the selected metric values from the digest rows.
// Uses day-index as x (0-based). Returns the slope in units-per-day.
// Returns 0.0 for fewer than 2 data points.
func ComputeTrendSlope(rows []DigestRow, pctFunc func(DigestRow) int64) float64 {
	n := len(rows)
	if n < 2 {
		return 0.0
	}

	var sumX, sumY, sumXY, sumX2 float64
	for i, row := range rows {
		x := float64(i)
		y := float64(pctFunc(row))
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	nf := float64(n)
	denom := nf*sumX2 - sumX*sumX
	if denom == 0 {
		return 0.0
	}
	return (nf*sumXY - sumX*sumY) / denom
}
