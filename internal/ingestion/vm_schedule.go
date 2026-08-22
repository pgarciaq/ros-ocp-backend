package ingestion

import "github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"

// VMBusinessHoursWeight returns drop-or-full weights for 15-minute VM samples.
// Weight <= 0 drops the sample; otherwise the full sample is included (no
// fractional percentile vote). Disabled namespace schedules yield 0.
func VMBusinessHoursWeight(cache *bhschedule.Cache) func(VMRow) float64 {
	return func(r VMRow) float64 {
		if cache == nil {
			return 0
		}
		sched := cache.Resolve(r.Namespace)
		if !sched.Enabled {
			return 0
		}
		return bhschedule.ScheduleWeight(r.IntervalStart, sched)
	}
}

// VMGPUDeviceBusinessHoursWeight is drop-or-full weighting for GPU device CSV rows.
func VMGPUDeviceBusinessHoursWeight(cache *bhschedule.Cache) func(VMGPUDeviceRow) float64 {
	return func(r VMGPUDeviceRow) float64 {
		if cache == nil {
			return 0
		}
		sched := cache.Resolve(r.Namespace)
		if !sched.Enabled {
			return 0
		}
		return bhschedule.ScheduleWeight(r.IntervalStart, sched)
	}
}
