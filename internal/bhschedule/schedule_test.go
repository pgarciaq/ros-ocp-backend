package bhschedule

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitLocation_CachesTimezone(t *testing.T) {
	s := Schedule{
		Enabled:   true,
		Timezone:  "America/New_York",
		Days:      []string{"tuesday"},
		StartTime: "08:00",
		EndTime:   "17:00",
	}
	require.NoError(t, s.InitLocation())
	require.NotNil(t, s.loc)
	assert.Equal(t, "America/New_York", s.location().String())

	interval := time.Date(2026, 1, 6, 15, 0, 0, 0, time.UTC) // Tue 10:00 ET
	assert.True(t, InBusinessHours(interval, s))
}

func TestInitLocation_DisabledSkipsLoad(t *testing.T) {
	s := Schedule{Enabled: false, Timezone: "America/New_York"}
	require.NoError(t, s.InitLocation())
	assert.Nil(t, s.loc)
}

func TestInitLocation_InvalidTimezone(t *testing.T) {
	s := Schedule{Enabled: true, Timezone: "Not/A_Zone"}
	err := s.InitLocation()
	require.Error(t, err)
	assert.Nil(t, s.loc)
}

func TestInBusinessHours_SameDaySchedule(t *testing.T) {
	s := Schedule{
		Enabled:   true,
		Timezone:  "UTC",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "09:00",
		EndTime:   "17:00",
	}
	require.NoError(t, s.InitLocation())

	tests := []struct {
		name   string
		time   time.Time
		expect bool
	}{
		{"before start", time.Date(2026, 1, 5, 8, 59, 0, 0, time.UTC), false},   // Mon 08:59
		{"at start", time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), true},         // Mon 09:00
		{"mid-day", time.Date(2026, 1, 5, 12, 30, 0, 0, time.UTC), true},        // Mon 12:30
		{"before end", time.Date(2026, 1, 5, 16, 59, 0, 0, time.UTC), true},     // Mon 16:59
		{"at end (exclusive)", time.Date(2026, 1, 5, 17, 0, 0, 0, time.UTC), false}, // Mon 17:00
		{"after end", time.Date(2026, 1, 5, 20, 0, 0, 0, time.UTC), false},      // Mon 20:00
		{"weekend", time.Date(2026, 1, 4, 12, 0, 0, 0, time.UTC), false},        // Sun 12:00
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expect, InBusinessHours(tc.time, s))
		})
	}
}

func TestInBusinessHours_OvernightSchedule(t *testing.T) {
	s := Schedule{
		Enabled:   true,
		Timezone:  "UTC",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "22:00",
		EndTime:   "06:00",
	}
	require.NoError(t, s.InitLocation())

	tests := []struct {
		name   string
		time   time.Time
		expect bool
	}{
		{"before start (daytime)", time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), false}, // Mon 12:00
		{"at start", time.Date(2026, 1, 5, 22, 0, 0, 0, time.UTC), true},               // Mon 22:00
		{"late night", time.Date(2026, 1, 5, 23, 30, 0, 0, time.UTC), true},             // Mon 23:30
		{"early morning (before end)", time.Date(2026, 1, 6, 3, 0, 0, 0, time.UTC), true}, // Tue 03:00
		{"at end (exclusive)", time.Date(2026, 1, 6, 6, 0, 0, 0, time.UTC), false},      // Tue 06:00
		{"after end", time.Date(2026, 1, 6, 8, 0, 0, 0, time.UTC), false},               // Tue 08:00
		{"just before start", time.Date(2026, 1, 5, 21, 59, 0, 0, time.UTC), false},     // Mon 21:59
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expect, InBusinessHours(tc.time, s))
		})
	}
}

func TestInBusinessHours_MidnightToMidnight(t *testing.T) {
	s := Schedule{
		Enabled:   true,
		Timezone:  "UTC",
		Days:      []string{"monday"},
		StartTime: "00:00",
		EndTime:   "00:00",
	}
	require.NoError(t, s.InitLocation())

	// startMin == endMin == 0: same-day path with startMin <= endMin,
	// condition is localMin >= 0 && localMin < 0 → always false.
	// This represents "no hours" (zero-width window), not "all hours".
	assert.False(t, InBusinessHours(time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), s))
}

func TestInBusinessHours_FullDay(t *testing.T) {
	// Full day: 00:00–23:59 (almost 24h, same-day)
	s := Schedule{
		Enabled:   true,
		Timezone:  "UTC",
		Days:      []string{"monday"},
		StartTime: "00:00",
		EndTime:   "23:59",
	}
	require.NoError(t, s.InitLocation())

	assert.True(t, InBusinessHours(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), s))
	assert.True(t, InBusinessHours(time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), s))
	assert.True(t, InBusinessHours(time.Date(2026, 1, 5, 23, 58, 0, 0, time.UTC), s))
	// 23:59 is exclusive end
	assert.False(t, InBusinessHours(time.Date(2026, 1, 5, 23, 59, 0, 0, time.UTC), s))
}

func TestInBusinessHours_DisabledSchedule(t *testing.T) {
	s := Schedule{Enabled: false}
	assert.False(t, InBusinessHours(time.Now(), s))
}

func TestInBusinessHours_NoTimezone(t *testing.T) {
	s := Schedule{
		Enabled:   true,
		Timezone:  "",
		Days:      []string{"monday"},
		StartTime: "09:00",
		EndTime:   "17:00",
	}
	// No location → always false
	assert.False(t, InBusinessHours(time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), s))
}

func TestInBusinessHours_WrongDay(t *testing.T) {
	s := Schedule{
		Enabled:   true,
		Timezone:  "UTC",
		Days:      []string{"tuesday"},
		StartTime: "09:00",
		EndTime:   "17:00",
	}
	require.NoError(t, s.InitLocation())

	// Monday — wrong day
	assert.False(t, InBusinessHours(time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), s))
}

func TestScheduleWeight_BusinessHours(t *testing.T) {
	s := Schedule{
		Enabled:        true,
		Timezone:       "UTC",
		Days:           []string{"monday"},
		StartTime:      "09:00",
		EndTime:        "17:00",
		OffHoursWeight: 0.5,
	}
	require.NoError(t, s.InitLocation())

	// In business hours → weight 1.0
	assert.Equal(t, 1.0, ScheduleWeight(time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), s))
	// Outside business hours → OffHoursWeight
	assert.Equal(t, 0.5, ScheduleWeight(time.Date(2026, 1, 5, 20, 0, 0, 0, time.UTC), s))
}

func TestScheduleWeight_Disabled(t *testing.T) {
	s := Schedule{Enabled: false}
	assert.Equal(t, 1.0, ScheduleWeight(time.Now(), s))
}
