package ingestion

// ScheduleType matches digest_schedule_type enum values in PostgreSQL.
type ScheduleType string

const (
	ScheduleTypeAllHours      ScheduleType = "all_hours"
	ScheduleTypeBusinessHours ScheduleType = "business_hours"
)
