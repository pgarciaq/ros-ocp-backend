// Business-hours schedule resolution loads rows from business_hours_schedules
// and resolves org → cluster → namespace inheritance.
package engine

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
)

// OrgClusterSentinelUUID is the cluster_uuid value for org-wide default rows.
const OrgClusterSentinelUUID = bhschedule.OrgClusterSentinelUUID

// AllHoursSchedule returns a disabled placeholder (all-hours-only behavior).
func AllHoursSchedule() BusinessHoursSchedule {
	return bhschedule.AllHoursSchedule()
}

// ScheduleCache holds schedules for one ingestion batch (org + cluster).
type ScheduleCache = bhschedule.Cache

// LoadSchedules loads all schedule rows for orgID and clusterUUID (including org default).
func LoadSchedules(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (*ScheduleCache, error) {
	return bhschedule.LoadSchedules(ctx, pool, orgID, clusterUUID)
}

// UpsertBusinessHoursSchedule inserts or updates a schedule row at the given scope.
func UpsertBusinessHoursSchedule(ctx context.Context, pool *pgxpool.Pool, sched BusinessHoursSchedule) error {
	return bhschedule.UpsertSchedule(ctx, pool, sched)
}

// DeleteBusinessHoursSchedule removes a schedule row at the given scope.
func DeleteBusinessHoursSchedule(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, namespace string) error {
	return bhschedule.DeleteSchedule(ctx, pool, orgID, clusterUUID, namespace)
}

// Re-export pgx.ErrNoRows for callers that compare delete outcomes.
var ErrNoRows = pgx.ErrNoRows
