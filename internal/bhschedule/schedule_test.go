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
