package ingestion

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
)

func weekdaySchedule() bhschedule.Schedule {
	return bhschedule.Schedule{
		Timezone:       "America/New_York",
		Days:           []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime:      "08:00",
		EndTime:        "17:00",
		OffHoursWeight: 0.0,
		Enabled:        true,
	}
}

func TestScheduleWeight_OffHoursZero(t *testing.T) {
	sched := weekdaySchedule()
	sat := time.Date(2026, 1, 10, 15, 0, 0, 0, time.UTC)
	assert.InDelta(t, 0.0, bhschedule.ScheduleWeight(sat, sched), 0.001)
}

func TestScheduleWeight_OffHoursPartial(t *testing.T) {
	sched := weekdaySchedule()
	sched.OffHoursWeight = 0.2
	sat := time.Date(2026, 1, 10, 15, 0, 0, 0, time.UTC)
	assert.InDelta(t, 0.2, bhschedule.ScheduleWeight(sat, sched), 0.001)
}

func TestScheduleWeight_AllHoursEquivalent(t *testing.T) {
	disabled := bhschedule.AllHoursSchedule()
	anyTime := time.Date(2026, 1, 10, 15, 0, 0, 0, time.UTC)
	assert.InDelta(t, 1.0, bhschedule.ScheduleWeight(anyTime, disabled), 0.001)
}
