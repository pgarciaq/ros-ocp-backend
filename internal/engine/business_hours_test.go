package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func nyWeekdaySchedule() BusinessHoursSchedule {
	return BusinessHoursSchedule{
		Timezone:  "America/New_York",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "08:00",
		EndTime:   "17:00",
		Enabled:   true,
	}
}

func TestInBusinessHours_WeekdayInside(t *testing.T) {
	// Tue 10:00 America/New_York = 2026-01-06 15:00 UTC (EST, UTC-5)
	interval := time.Date(2026, 1, 6, 15, 0, 0, 0, time.UTC)
	assert.True(t, InBusinessHours(interval, nyWeekdaySchedule()))
}

func TestInBusinessHours_IntervalStartOnly_PartialOverlap(t *testing.T) {
	sched := BusinessHoursSchedule{
		Timezone:  "America/New_York",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "07:50",
		EndTime:   "17:00",
		Enabled:   true,
	}
	// Tue 07:45 local (before window) → false under start-only rule
	interval := time.Date(2026, 1, 6, 12, 45, 0, 0, time.UTC)
	assert.False(t, InBusinessHours(interval, sched))
}

func TestInBusinessHours_SaturdayOutside(t *testing.T) {
	// Sat 10:00 America/New_York = 2026-01-10 15:00 UTC
	interval := time.Date(2026, 1, 10, 15, 0, 0, 0, time.UTC)
	assert.False(t, InBusinessHours(interval, nyWeekdaySchedule()))
}

func TestInBusinessHours_StartInclusive(t *testing.T) {
	// Tue 08:00 America/New_York = 2026-01-06 13:00 UTC
	interval := time.Date(2026, 1, 6, 13, 0, 0, 0, time.UTC)
	assert.True(t, InBusinessHours(interval, nyWeekdaySchedule()))
}

func TestInBusinessHours_EndExclusive(t *testing.T) {
	// Tue 17:00 local → false; 16:59 → true
	atEnd := time.Date(2026, 1, 6, 22, 0, 0, 0, time.UTC)
	assert.False(t, InBusinessHours(atEnd, nyWeekdaySchedule()))

	beforeEnd := time.Date(2026, 1, 6, 21, 59, 0, 0, time.UTC)
	assert.True(t, InBusinessHours(beforeEnd, nyWeekdaySchedule()))
}

func TestInBusinessHours_AsiaKolkata(t *testing.T) {
	sched := BusinessHoursSchedule{
		Timezone:  "Asia/Kolkata",
		Days:      []string{"monday"},
		StartTime: "10:00",
		EndTime:   "18:00",
		Enabled:   true,
	}
	// Mon 10:30 IST (+05:30) = Mon 05:00 UTC
	interval := time.Date(2026, 1, 5, 5, 0, 0, 0, time.UTC)
	assert.True(t, InBusinessHours(interval, sched))
}

func TestInBusinessHours_PacificChatham(t *testing.T) {
	sched := BusinessHoursSchedule{
		Timezone:  "Pacific/Chatham",
		Days:      []string{"monday"},
		StartTime: "09:00",
		EndTime:   "17:00",
		Enabled:   true,
	}
	// Mon 10:00 Chatham (+12:45) ≈ Sun 21:15 UTC — use known offset case:
	// Mon 2026-01-05 10:00 +1245 → 2026-01-04 21:15 UTC
	interval := time.Date(2026, 1, 4, 21, 15, 0, 0, time.UTC)
	assert.True(t, InBusinessHours(interval, sched))
}

func TestCombinedWeight_DesignExamples(t *testing.T) {
	// Design doc: 48h age, halflife 168, inside BH → W_decay≈0.821, W_schedule=1.0
	w1 := CombinedWeight(48, 168, 1.0)
	assert.InDelta(t, 0.821, w1, 0.01)

	// 120h age, halflife 168, off_hours 0.2 → W_decay≈0.607, W_final≈0.121
	w2 := CombinedWeight(120, 168, 0.2)
	assert.InDelta(t, 0.121, w2, 0.01)
}

func TestScheduleWeight_OffHoursZero(t *testing.T) {
	sched := nyWeekdaySchedule()
	sched.OffHoursWeight = 0.0
	sat := time.Date(2026, 1, 10, 15, 0, 0, 0, time.UTC)
	assert.InDelta(t, 0.0, ScheduleWeight(sat, sched), 0.001)
}

func TestScheduleWeight_OffHoursPartial(t *testing.T) {
	sched := nyWeekdaySchedule()
	sched.OffHoursWeight = 0.2
	sat := time.Date(2026, 1, 10, 15, 0, 0, 0, time.UTC)
	assert.InDelta(t, 0.2, ScheduleWeight(sat, sched), 0.001)
}

func TestScheduleWeight_AllHoursEquivalent(t *testing.T) {
	disabled := AllHoursSchedule()
	anyTime := time.Date(2026, 1, 10, 15, 0, 0, 0, time.UTC)
	assert.InDelta(t, 1.0, ScheduleWeight(anyTime, disabled), 0.001)
}

func TestScheduleWeight_AllHoursStreamAlwaysOne(t *testing.T) {
	sched := BusinessHoursSchedule{
		Timezone:       "America/New_York",
		Days:           []string{"monday"},
		StartTime:      "08:00",
		EndTime:        "17:00",
		OffHoursWeight: 0.0,
		Enabled:        true,
	}
	// Saturday outside BH window — all_hours stream still 1.0
	sat := time.Date(2026, 1, 10, 15, 0, 0, 0, time.UTC)
	assert.InDelta(t, 1.0, ScheduleWeightForStream(sat, sched, true), 0.001)
}

func TestInBusinessHours_AllSevenDays(t *testing.T) {
	sched := BusinessHoursSchedule{
		Timezone:  "UTC",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime: "08:00",
		EndTime:   "20:00",
		Enabled:   true,
	}
	// Each weekday at noon UTC
	dates := []time.Time{
		time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC),  // Mon
		time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC),  // Tue
		time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC),  // Wed
		time.Date(2026, 1, 8, 12, 0, 0, 0, time.UTC),  // Thu
		time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC),  // Fri
		time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), // Sat
		time.Date(2026, 1, 11, 12, 0, 0, 0, time.UTC), // Sun
	}
	for _, d := range dates {
		assert.True(t, InBusinessHours(d, sched), "expected inside window at %v", d)
	}
}

func TestInBusinessHours_SundayOnly(t *testing.T) {
	sched := BusinessHoursSchedule{
		Timezone:  "UTC",
		Days:      []string{"sunday"},
		StartTime: "08:00",
		EndTime:   "20:00",
		Enabled:   true,
	}
	sat := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	sun := time.Date(2026, 1, 11, 12, 0, 0, 0, time.UTC)
	assert.False(t, InBusinessHours(sat, sched))
	assert.True(t, InBusinessHours(sun, sched))
}
