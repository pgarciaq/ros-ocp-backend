package container

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// EvaluateNotificationsWithThresholds produces notification codes using explicit thresholds.
// It always returns a non-nil slice (empty []int16{} when no conditions match) because
// the database column notification_codes has a NOT NULL constraint.
func EvaluateNotificationsWithThresholds(rec types.ContainerRec, minDataDays int, th types.NotificationThresholds) []int16 {
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
	if rec.OOMCountSum > 0 {
		codes = append(codes, types.NotifOOMDetected)
	}
	if rec.IdleState.IsIdleOrZombie() {
		codes = append(codes, types.NotifIdleWorkload)
	}
	if rec.Stale {
		codes = append(codes, types.NotifStaleData)
	}
	if rec.MemTrendSlope > th.MemTrendSlopeThreshold {
		codes = append(codes, types.NotifMemoryTrendingUp)
	}

	return codes
}
