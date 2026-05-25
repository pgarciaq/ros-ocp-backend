package engine

import "github.com/redhatinsights/ros-ocp-backend/internal/fixedpoint"

// BasisPointsScale represents 100% utilization (1.0) in fixed-point basis points.
const BasisPointsScale = fixedpoint.BasisPointsScale

// FloatToBasisPoints converts a 0.0-1.0 utilization fraction to basis points.
func FloatToBasisPoints(v float64) int32 {
	return fixedpoint.FloatToBasisPoints(v)
}

// BasisPointsToFloat converts basis points back to a 0.0-1.0 fraction.
func BasisPointsToFloat(bp int32) float64 {
	return fixedpoint.BasisPointsToFloat(bp)
}

// BasisPointsToFloat32 converts basis points to float32 for API responses.
func BasisPointsToFloat32(bp int32) float32 {
	return fixedpoint.BasisPointsToFloat32(bp)
}

// ThresholdToBasisPoints converts a threshold float (e.g. 0.02) to basis points.
func ThresholdToBasisPoints(th float64) int32 {
	return FloatToBasisPoints(th)
}
