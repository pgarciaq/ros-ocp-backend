package ingestion

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// BH-UNIT-113: Go constants must match digest_schedule_type SQL enum labels.
func TestScheduleTypeConstants_MatchEnum(t *testing.T) {
	assert.Equal(t, "all_hours", string(ScheduleTypeAllHours))
	assert.Equal(t, "business_hours", string(ScheduleTypeBusinessHours))
}
