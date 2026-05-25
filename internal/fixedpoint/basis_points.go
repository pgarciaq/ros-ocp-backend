package fixedpoint

import "math"

// BasisPointsScale represents 100% (1.0) utilization.
const BasisPointsScale int32 = 10000

// FloatToBasisPoints converts a 0.0-1.0 fraction to basis points.
func FloatToBasisPoints(v float64) int32 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return BasisPointsScale
	}
	return int32(math.Round(v * float64(BasisPointsScale)))
}

// BasisPointsToFloat converts basis points to a 0.0-1.0 fraction.
func BasisPointsToFloat(bp int32) float64 {
	return float64(bp) / float64(BasisPointsScale)
}

// BasisPointsToFloat32 converts basis points to float32 fraction for API output.
func BasisPointsToFloat32(bp int32) float32 {
	return float32(BasisPointsToFloat(bp))
}
