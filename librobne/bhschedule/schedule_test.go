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
		{"before start", time.Date(2026, 1, 5, 8, 59, 0, 0, time.UTC), false},       // Mon 08:59
		{"at start", time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), true},             // Mon 09:00
		{"mid-day", time.Date(2026, 1, 5, 12, 30, 0, 0, time.UTC), true},            // Mon 12:30
		{"before end", time.Date(2026, 1, 5, 16, 59, 0, 0, time.UTC), true},         // Mon 16:59
		{"at end (exclusive)", time.Date(2026, 1, 5, 17, 0, 0, 0, time.UTC), false}, // Mon 17:00
		{"after end", time.Date(2026, 1, 5, 20, 0, 0, 0, time.UTC), false},          // Mon 20:00
		{"weekend", time.Date(2026, 1, 4, 12, 0, 0, 0, time.UTC), false},            // Sun 12:00
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
		{"before start (daytime)", time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), false},   // Mon 12:00
		{"at start", time.Date(2026, 1, 5, 22, 0, 0, 0, time.UTC), true},                  // Mon 22:00
		{"late night", time.Date(2026, 1, 5, 23, 30, 0, 0, time.UTC), true},               // Mon 23:30
		{"early morning (before end)", time.Date(2026, 1, 6, 3, 0, 0, 0, time.UTC), true}, // Tue 03:00
		{"at end (exclusive)", time.Date(2026, 1, 6, 6, 0, 0, 0, time.UTC), false},        // Tue 06:00
		{"after end", time.Date(2026, 1, 6, 8, 0, 0, 0, time.UTC), false},                 // Tue 08:00
		{"just before start", time.Date(2026, 1, 5, 21, 59, 0, 0, time.UTC), false},       // Mon 21:59
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

func TestInBusinessHours_OvernightSingleDay(t *testing.T) {
	// Only Monday configured with overnight schedule (22:00–06:00).
	// The post-midnight portion (Tue 00:00–05:59) should count because
	// it's Monday's overnight shift extending into Tuesday.
	s := Schedule{
		Enabled:   true,
		Timezone:  "UTC",
		Days:      []string{"monday"},
		StartTime: "22:00",
		EndTime:   "06:00",
	}
	require.NoError(t, s.InitLocation())

	tests := []struct {
		name   string
		time   time.Time
		expect bool
	}{
		{"Mon 22:00 (shift start)", time.Date(2026, 1, 5, 22, 0, 0, 0, time.UTC), true},
		{"Mon 23:30 (pre-midnight)", time.Date(2026, 1, 5, 23, 30, 0, 0, time.UTC), true},
		{"Tue 03:00 (post-midnight, prev day=Mon)", time.Date(2026, 1, 6, 3, 0, 0, 0, time.UTC), true},
		{"Tue 05:59 (just before end)", time.Date(2026, 1, 6, 5, 59, 0, 0, time.UTC), true},
		{"Tue 06:00 (end, exclusive)", time.Date(2026, 1, 6, 6, 0, 0, 0, time.UTC), false},
		{"Mon 12:00 (daytime, not in window)", time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), false},
		{"Mon 21:59 (just before shift)", time.Date(2026, 1, 5, 21, 59, 0, 0, time.UTC), false},
		{"Sun 23:00 (Sun not in days)", time.Date(2026, 1, 4, 23, 0, 0, 0, time.UTC), false},
		{"Mon 03:00 (post-midnight, prev day=Sun not in days)", time.Date(2026, 1, 5, 3, 0, 0, 0, time.UTC), false},
		{"Tue 22:00 (Tue not in days, pre-midnight)", time.Date(2026, 1, 6, 22, 0, 0, 0, time.UTC), false},
		{"Wed 03:00 (prev day=Tue not in days)", time.Date(2026, 1, 7, 3, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expect, InBusinessHours(tc.time, s))
		})
	}
}

func TestInBusinessHours_OvernightFridayIntoSaturday(t *testing.T) {
	// Mon-Fri overnight schedule: Friday's shift should extend into Saturday morning.
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
		{"Fri 22:00 (shift start)", time.Date(2026, 1, 9, 22, 0, 0, 0, time.UTC), true},
		{"Sat 03:00 (prev=Fri, in days)", time.Date(2026, 1, 10, 3, 0, 0, 0, time.UTC), true},
		{"Sat 22:00 (Sat not in days)", time.Date(2026, 1, 10, 22, 0, 0, 0, time.UTC), false},
		{"Sun 03:00 (prev=Sat, not in days)", time.Date(2026, 1, 11, 3, 0, 0, 0, time.UTC), false},
		{"Mon 03:00 (prev=Sun, not in days)", time.Date(2026, 1, 12, 3, 0, 0, 0, time.UTC), false},
		{"Mon 22:00 (Mon in days)", time.Date(2026, 1, 12, 22, 0, 0, 0, time.UTC), true},
		{"Tue 03:00 (prev=Mon, in days)", time.Date(2026, 1, 13, 3, 0, 0, 0, time.UTC), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expect, InBusinessHours(tc.time, s))
		})
	}
}

func TestInBusinessHours_DSTOvernightWallClock(t *testing.T) {
	// US/EU DST moves on Sunday. Mon–Fri overnight 22:00–06:00 does not include
	// Sunday morning, so these cases use Saturday-in-days overnight so the
	// skipped/repeated hour is in-window. Classification is wall clock, not
	// elapsed duration (#507).
	t.Parallel()
	type row struct {
		name     string
		utc      time.Time
		wantHour int
		wantMin  int
		expect   bool
	}
	run := func(t *testing.T, tz string, cases []row) {
		t.Helper()
		loc, err := time.LoadLocation(tz)
		require.NoError(t, err)
		s := Schedule{
			Enabled:   true,
			Timezone:  tz,
			Days:      []string{"saturday"},
			StartTime: "22:00",
			EndTime:   "06:00",
		}
		require.NoError(t, s.InitLocation())
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				local := tc.utc.In(loc)
				assert.Equal(t, tc.wantHour, local.Hour(), "local hour")
				assert.Equal(t, tc.wantMin, local.Minute(), "local minute")
				assert.Equal(t, tc.expect, InBusinessHours(tc.utc, s))
			})
		}
	}

	t.Run("America/New_York spring-forward 2026-03-08", func(t *testing.T) {
		run(t, "America/New_York", []row{
			{"Sat 22:00 EST start", time.Date(2026, 3, 8, 3, 0, 0, 0, time.UTC), 22, 0, true},
			{"Sun 01:30 EST before gap", time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC), 1, 30, true},
			{"Sun 03:00 EDT after gap still in window", time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC), 3, 0, true},
			{"Sun 06:00 EDT exclusive end", time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC), 6, 0, false},
			{"Sun 22:00 EDT Sunday not in days", time.Date(2026, 3, 9, 2, 0, 0, 0, time.UTC), 22, 0, false},
		})
	})
	t.Run("America/New_York fall-back 2026-11-01", func(t *testing.T) {
		run(t, "America/New_York", []row{
			{"Sat 22:00 EDT start", time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC), 22, 0, true},
			{"Sun 01:30 EDT first occurrence", time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC), 1, 30, true},
			{"Sun 01:30 EST second occurrence", time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC), 1, 30, true},
			{"Sun 06:00 EST exclusive end", time.Date(2026, 11, 1, 11, 0, 0, 0, time.UTC), 6, 0, false},
		})
	})
	t.Run("Europe/Madrid spring-forward 2026-03-29", func(t *testing.T) {
		run(t, "Europe/Madrid", []row{
			{"Sat 22:00 CET start", time.Date(2026, 3, 28, 21, 0, 0, 0, time.UTC), 22, 0, true},
			{"Sun 01:30 CET before gap", time.Date(2026, 3, 29, 0, 30, 0, 0, time.UTC), 1, 30, true},
			{"Sun 03:00 CEST after gap still in window", time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC), 3, 0, true},
			{"Sun 06:00 CEST exclusive end", time.Date(2026, 3, 29, 4, 0, 0, 0, time.UTC), 6, 0, false},
		})
	})
	t.Run("Europe/Madrid fall-back 2026-10-25", func(t *testing.T) {
		run(t, "Europe/Madrid", []row{
			{"Sat 22:00 CEST start", time.Date(2026, 10, 24, 20, 0, 0, 0, time.UTC), 22, 0, true},
			{"Sun 02:30 CEST first occurrence", time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), 2, 30, true},
			{"Sun 02:30 CET second occurrence", time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC), 2, 30, true},
			{"Sun 06:00 CET exclusive end", time.Date(2026, 10, 25, 5, 0, 0, 0, time.UTC), 6, 0, false},
		})
	})
}

func TestPreviousWeekday(t *testing.T) {
	tests := []struct {
		input  time.Weekday
		expect time.Weekday
	}{
		{time.Sunday, time.Saturday},
		{time.Monday, time.Sunday},
		{time.Tuesday, time.Monday},
		{time.Wednesday, time.Tuesday},
		{time.Thursday, time.Wednesday},
		{time.Friday, time.Thursday},
		{time.Saturday, time.Friday},
	}
	for _, tc := range tests {
		t.Run(tc.input.String(), func(t *testing.T) {
			assert.Equal(t, tc.expect, previousWeekday(tc.input))
		})
	}
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
