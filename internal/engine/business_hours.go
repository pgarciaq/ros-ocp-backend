package engine

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
)

// BusinessHoursSchedule is the effective schedule used for ingestion weighting.
type BusinessHoursSchedule = bhschedule.Schedule

// InBusinessHours reports whether intervalStart (UTC) falls inside the schedule window.
func InBusinessHours(intervalStart time.Time, schedule BusinessHoursSchedule) bool {
	return bhschedule.InBusinessHours(intervalStart, schedule)
}

// ScheduleWeight returns W_schedule for business-hours digest weighting.
func ScheduleWeight(intervalStart time.Time, schedule BusinessHoursSchedule) float64 {
	return bhschedule.ScheduleWeight(intervalStart, schedule)
}

// ScheduleWeightForStream returns W_schedule for the given digest schedule type.
func ScheduleWeightForStream(intervalStart time.Time, schedule BusinessHoursSchedule, allHoursStream bool) float64 {
	return bhschedule.ScheduleWeightForStream(intervalStart, schedule, allHoursStream)
}

// CombinedWeight computes W_final = W_decay × W_schedule.
func CombinedWeight(ageHours, decayHalfLifeHours, scheduleWeight float64) float64 {
	return DecayWeight(ageHours, decayHalfLifeHours) * scheduleWeight
}
