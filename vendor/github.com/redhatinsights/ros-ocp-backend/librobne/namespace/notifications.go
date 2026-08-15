package namespace

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// EvaluateNamespaceNotifications produces notification codes using compiled
// namespace sizing defaults.
func EvaluateNamespaceNotifications(rec NamespaceRec) []int16 {
	return EvaluateNamespaceNotificationsWithThresholds(
		rec,
		types.NotificationThresholdsFromSizing(DefaultNamespaceSizingThresholds()),
	)
}

// EvaluateNamespaceNotificationsWithThresholds produces namespace notification
// codes using explicit thresholds.
func EvaluateNamespaceNotificationsWithThresholds(rec NamespaceRec, th types.NotificationThresholds) []int16 {
	codes := []int16{}

	if rec.DataDays < 1 {
		codes = append(codes, types.NotifNewWorkload)
	}
	if rec.ConfidenceLevel < th.LowConfidenceThreshold && rec.DataDays > 0 {
		codes = append(codes, types.NotifLowConfidence)
	}
	if rec.DataDays > 0 && rec.DataDays <= th.SparseDataThreshold {
		codes = append(codes, types.NotifSparseData)
	}
	if rec.MemTrendSlope > th.MemTrendSlopeThreshold {
		codes = append(codes, types.NotifMemoryTrendingUp)
	}
	if rec.Stale {
		codes = append(codes, types.NotifStaleData)
	}

	return codes
}
