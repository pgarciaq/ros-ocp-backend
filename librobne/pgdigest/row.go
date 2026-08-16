package pgdigest

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// ScheduleAllHours is the digest_schedule_type the CLI writes for the unweighted stream.
const ScheduleAllHours = "all_hours"

// ScheduleBusinessHours is the weighted business-hours digest stream.
const ScheduleBusinessHours = "business_hours"

// Row is one already-computed daily digest plus identity and schedule_type.
// Processor sets ScheduleType per key so INSERT order can match the unique index.
type Row struct {
	OrgID        string
	ClusterUUID  string
	ScheduleType string
	Digest       types.KeyedDigest
}
