// Package bhschedule provides business-hours schedule evaluation shared by
// ingestion and the recommendation engine (avoids ingestion↔engine import cycles).
package bhschedule

import (
	"fmt"
	"strings"
	"time"
)

var weekdayToName = []string{
	"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday",
}

// Schedule is the effective business-hours configuration for ingestion weighting.
type Schedule struct {
	OrgID          string
	ClusterUUID    string
	Namespace      string
	Timezone       string
	Days           []string
	StartTime      string // HH:MM local wall clock
	EndTime        string // HH:MM local wall clock
	OffHoursWeight float64
	Enabled        bool
}

// AllHoursSchedule returns a disabled placeholder (all-hours-only behavior).
func AllHoursSchedule() Schedule {
	return Schedule{Enabled: false}
}

// InBusinessHours reports whether intervalStart (UTC) falls inside the schedule's
// local day-of-week and half-open time window [start_time, end_time).
func InBusinessHours(intervalStart time.Time, schedule Schedule) bool {
	if !schedule.Enabled {
		return false
	}

	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return false
	}

	local := intervalStart.In(loc)
	if !dayAllowed(local.Weekday(), schedule.Days) {
		return false
	}

	startMin, err := parseHHMM(schedule.StartTime)
	if err != nil {
		return false
	}
	endMin, err := parseHHMM(schedule.EndTime)
	if err != nil {
		return false
	}

	localMin := local.Hour()*60 + local.Minute()
	return localMin >= startMin && localMin < endMin
}

// ScheduleWeight returns W_schedule for business-hours digest weighting.
func ScheduleWeight(intervalStart time.Time, schedule Schedule) float64 {
	if !schedule.Enabled {
		return 1.0
	}
	if InBusinessHours(intervalStart, schedule) {
		return 1.0
	}
	return schedule.OffHoursWeight
}

// ScheduleWeightForStream returns W_schedule for the given digest schedule type.
func ScheduleWeightForStream(intervalStart time.Time, schedule Schedule, allHoursStream bool) float64 {
	if allHoursStream {
		return 1.0
	}
	return ScheduleWeight(intervalStart, schedule)
}

func dayAllowed(weekday time.Weekday, days []string) bool {
	if len(days) == 0 {
		return false
	}
	name := weekdayToName[weekday]
	for _, d := range days {
		if d == name {
			return true
		}
	}
	return false
}

func parseHHMM(s string) (int, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	var h, m int
	if _, err := fmt.Sscanf(parts[0], "%d", &h); err != nil {
		return 0, fmt.Errorf("invalid hour in %q", s)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &m); err != nil {
		return 0, fmt.Errorf("invalid minute in %q", s)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("time out of range %q", s)
	}
	return h*60 + m, nil
}
