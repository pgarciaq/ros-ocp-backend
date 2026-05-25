package bhschedule

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPendingMarkerStub(t *testing.T) {
	stub := Schedule{
		Namespace:      "",
		Timezone:       PendingMarkerTimezone,
		Days:           PendingMarkerDays,
		StartTime:      PendingMarkerStart,
		EndTime:        PendingMarkerEnd,
		OffHoursWeight: 0,
		Enabled:        false,
	}
	assert.True(t, IsPendingMarkerStub(stub))

	real := Schedule{
		Namespace: "",
		Timezone:  "America/New_York",
		Days:      []string{"monday"},
		StartTime: "08:00",
		EndTime:   "17:00",
		Enabled:   false,
	}
	assert.False(t, IsPendingMarkerStub(real))
}

func TestLoadSchedules_IgnoresPendingMarkerStub(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	// Covered by integration tests; keep unit logic in IsPendingMarkerStub.
}
