package bhschedule

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	weekdayToName = []string{
		"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday",
	}
	// timezoneLocations caches IANA zones for schedules constructed without InitLocation (tests).
	timezoneLocations sync.Map
)

// Schedule is the effective business-hours configuration for digest weighting.
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
	loc            *time.Location // set by InitLocation at load time
	boundsReady    bool           // startMin/endMin parsed at load time
	startMin       int            // minutes since midnight (local wall clock)
	endMin         int
}

// AllHoursSchedule returns a disabled placeholder (all-hours-only behavior).
func AllHoursSchedule() Schedule {
	return Schedule{Enabled: false}
}

// InitLocation loads and caches the IANA timezone for enabled schedules.
// Call once after constructing a Schedule outside LoadSchedules (e.g. tests).
func (s *Schedule) InitLocation() error {
	return initScheduleLocation(s)
}

func initScheduleLocation(s *Schedule) error {
	if !s.Enabled {
		return nil
	}
	if s.Timezone != "" {
		loc, err := time.LoadLocation(s.Timezone)
		if err != nil {
			return err
		}
		s.loc = loc
	}
	startMin, err := parseHHMM(s.StartTime)
	if err != nil {
		return fmt.Errorf("start_time: %w", err)
	}
	endMin, err := parseHHMM(s.EndTime)
	if err != nil {
		return fmt.Errorf("end_time: %w", err)
	}
	s.startMin = startMin
	s.endMin = endMin
	s.boundsReady = true
	return nil
}

func (s Schedule) location() *time.Location {
	if s.loc != nil {
		return s.loc
	}
	if s.Timezone == "" {
		return nil
	}
	if v, ok := timezoneLocations.Load(s.Timezone); ok {
		return v.(*time.Location)
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil
	}
	timezoneLocations.Store(s.Timezone, loc)
	return loc
}

// InBusinessHours reports whether intervalStart (UTC) falls inside the schedule's
// local day-of-week and half-open time window [start_time, end_time).
// Comparison is local wall clock in the IANA zone (Hour*60+Minute), not elapsed
// duration: DST may skip or repeat an hour; the window still starts and ends at
// the configured HH:MM. For overnight schedules (start > end, e.g. 22:00–06:00),
// the post-midnight portion is attributed to the previous calendar day's shift.
func InBusinessHours(intervalStart time.Time, schedule Schedule) bool {
	if !schedule.Enabled {
		return false
	}

	loc := schedule.location()
	if loc == nil {
		return false
	}

	local := intervalStart.In(loc)

	startMin, endMin := schedule.startMin, schedule.endMin
	if !schedule.boundsReady {
		var err error
		startMin, err = parseHHMM(schedule.StartTime)
		if err != nil {
			return false
		}
		endMin, err = parseHHMM(schedule.EndTime)
		if err != nil {
			return false
		}
	}

	localMin := local.Hour()*60 + local.Minute()
	if startMin <= endMin {
		// Same-day schedule (e.g. 09:00–17:00): current day must be allowed.
		return dayAllowed(local.Weekday(), schedule.Days) &&
			localMin >= startMin && localMin < endMin
	}
	// Overnight schedule (e.g. 22:00–06:00):
	// Pre-midnight portion: current day must be allowed.
	if localMin >= startMin && dayAllowed(local.Weekday(), schedule.Days) {
		return true
	}
	// Post-midnight portion: the shift started on the previous calendar day.
	if localMin < endMin && dayAllowed(previousWeekday(local.Weekday()), schedule.Days) {
		return true
	}
	return false
}

func previousWeekday(d time.Weekday) time.Weekday {
	if d == time.Sunday {
		return time.Saturday
	}
	return d - 1
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

// DayAllowed reports whether the weekday is in the schedule's day list —
// whether the schedule's business-hours window applies to that day at all
// (#760: a weekday schedule's weekends have no in-window hours; the sleep
// predicate gates them on the off leg alone).
// DayIntersectsBusinessHours reports whether any instant of the UTC day
// starting at dayStart falls inside the schedule's business window. The
// hour-bucket classification (InBusinessHours on bucket starts) misses
// sub-hour windows contained within one clock hour — 09:15–09:45 contains
// no top-of-hour mark, so every day classifies zero BH buckets while real
// business time exists (#845 review P1: an applicability derived from
// bucket counts alone would skip the BH gates and qualify off-only idle
// evidence). This scan is minute-granular, matching the API's HH:MM
// window boundaries. Use for applicability only — coverage floors stay
// bucket-based (#827's lenient-by-construction choice, unchanged).
func DayIntersectsBusinessHours(dayStart time.Time, schedule Schedule) bool {
	if !schedule.Enabled {
		return false
	}
	for m := 0; m < 1440; m++ {
		if InBusinessHours(dayStart.Add(time.Duration(m)*time.Minute), schedule) {
			return true
		}
	}
	return false
}

func DayAllowed(weekday time.Weekday, days []string) bool {
	return dayAllowed(weekday, days)
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

// Fingerprint returns a stable identity of the schedule fields that
// determine the BH/off evidence split — timezone, day list, and window
// bounds (#831). It is the attestation stamped on sleep-evidence rows at
// ingest: the evaluator compares it against the CURRENTLY resolved
// schedule and reads unknown on mismatch, so evidence split under an old
// window can never support new advice under a changed one. Enabled-ness
// is deliberately excluded: a disable/re-enable with unchanged fields
// does not change the split.
func Fingerprint(s Schedule) string {
	startMin, startErr := parseHHMM(s.StartTime)
	endMin, endErr := parseHHMM(s.EndTime)
	if startErr != nil || endErr != nil {
		// Unparseable bounds degrade identically on both sides (the
		// writer skips disabled schedules; the evaluator's resolved row
		// is enabled by query) — a shared canonical zero keeps the
		// comparison total.
		startMin, endMin = 0, 0
	}
	return FingerprintBounds(s.Timezone, s.Days, startMin, endMin)
}

// FingerprintBounds is Fingerprint over the minute form — the server's
// resolved schedule carries minutes since midnight.
func FingerprintBounds(timezone string, days []string, startMin, endMin int) string {
	sorted := append([]string(nil), days...)
	sort.Strings(sorted)
	canonical := fmt.Sprintf("%s|%s|%02d:%02d-%02d:%02d",
		timezone, strings.Join(sorted, ","),
		startMin/60, startMin%60, endMin/60, endMin%60)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}
