package vm

import "strings"

// NormalizeInstanceTypeSeries maps KubeVirt instancetype class labels to ROS series names.
func NormalizeInstanceTypeSeries(class string) string {
	switch strings.TrimSpace(class) {
	case "compute-intensive":
		return vmSeriesComputeOptimized
	case "memory-intensive":
		return vmSeriesMemoryOptimized
	default:
		return vmSeriesGeneralPurpose
	}
}
