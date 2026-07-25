package container

import "github.com/redhatinsights/ros-ocp-backend/internal/engine/core"

const (
	defaultMemTrendSlopeThreshold         = 100.0
	defaultLowConfidenceThreshold float32 = 0.5
	defaultSparseDataThreshold    int     = 2
)

// EvaluateNotificationsWithThresholds produces notification codes using explicit thresholds.
func EvaluateNotificationsWithThresholds(rec core.ContainerRec, minDataDays int, th core.NotificationThresholds) []int16 {
	codes := []int16{}

	if rec.DataDays < 1 {
		codes = append(codes, core.NotifNewWorkload)
	}
	if rec.ConfidenceLevel < th.LowConfidenceThreshold && rec.DataDays > 0 {
		codes = append(codes, core.NotifLowConfidence)
	}
	if rec.DataDays > 0 && rec.DataDays <= th.SparseDataThreshold {
		codes = append(codes, core.NotifSparseData)
	}
	if rec.OOMCountSum > 0 {
		codes = append(codes, core.NotifOOMDetected)
	}
	if rec.IdleState.IsIdleOrZombie() {
		codes = append(codes, core.NotifIdleWorkload)
	}
	if rec.Stale {
		codes = append(codes, core.NotifStaleData)
	}
	if rec.MemTrendSlope > th.MemTrendSlopeThreshold {
		codes = append(codes, core.NotifMemoryTrendingUp)
	}

	return codes
}
