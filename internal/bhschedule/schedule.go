// Package bhschedule loads and caches business-hours rows from PostgreSQL.
// Window evaluation lives in librobne/bhschedule; this package type-aliases
// Schedule so ingest and the engine keep compiling.
package bhschedule

import (
	"time"

	libbh "github.com/redhatinsights/ros-ocp-backend/librobne/bhschedule"
)

// Schedule is the product alias for librobne schedule evaluation.
type Schedule = libbh.Schedule

// AllHoursSchedule returns a disabled placeholder (all-hours-only behavior).
func AllHoursSchedule() Schedule {
	return libbh.AllHoursSchedule()
}

// InBusinessHours reports whether intervalStart falls inside the schedule window.
// DayAllowed reports whether the weekday is in the schedule's day list
// (librobne wrapper; #760).
func DayAllowed(weekday time.Weekday, days []string) bool {
	return libbh.DayAllowed(weekday, days)
}

func InBusinessHours(intervalStart time.Time, schedule Schedule) bool {
	return libbh.InBusinessHours(intervalStart, schedule)
}

// ScheduleWeight returns W_schedule for business-hours digest weighting.
func ScheduleWeight(intervalStart time.Time, schedule Schedule) float64 {
	return libbh.ScheduleWeight(intervalStart, schedule)
}

// ScheduleWeightForStream returns W_schedule for the given digest schedule type.
func ScheduleWeightForStream(intervalStart time.Time, schedule Schedule, allHoursStream bool) float64 {
	return libbh.ScheduleWeightForStream(intervalStart, schedule, allHoursStream)
}

// Fingerprint returns the split-determining schedule identity (#831) —
// see the librobne implementation. Stamped on sleep-evidence rows at
// ingest; compared by the evaluator against the currently resolved
// schedule.
func Fingerprint(s Schedule) string {
	return libbh.Fingerprint(s)
}

// FingerprintBounds is Fingerprint over the minute form.
func FingerprintBounds(timezone string, days []string, startMin, endMin int) string {
	return libbh.FingerprintBounds(timezone, days, startMin, endMin)
}
