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
	caches, err := LoadSchedulesForClusters(ctx, pool, orgID, []string{clusterUUID})
	if err != nil {
		return nil, err
	}
	return caches[clusterUUID], nil
}

// LoadSchedulesForClusters loads schedules for multiple clusters in one query.
func LoadSchedulesForClusters(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string) (map[string]*ScheduleCache, error) {
	return bhschedule.LoadSchedulesForClusters(ctx, pool, orgID, clusterUUIDs)
}

// UpsertBusinessHoursSchedule inserts or updates a schedule row at the given scope.
func UpsertBusinessHoursSchedule(ctx context.Context, pool *pgxpool.Pool, sched BusinessHoursSchedule) error {
	return bhschedule.UpsertSchedule(ctx, pool, sched)
}

// DeleteBusinessHoursSchedule removes a schedule row at the given scope.
func DeleteBusinessHoursSchedule(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, namespace string) error {
	return bhschedule.DeleteSchedule(ctx, pool, orgID, clusterUUID, namespace)
}

// PruneClusterBusinessHoursDigests removes all business_hours digest rows for a cluster.
func PruneClusterBusinessHoursDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	return bhschedule.PruneClusterBusinessHoursDigests(ctx, pool, orgID, clusterUUID)
}

// PruneNamespaceBusinessHoursDigests removes business_hours digest rows for one namespace.
func PruneNamespaceBusinessHoursDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, namespace string) error {
	return bhschedule.PruneNamespaceBusinessHoursDigests(ctx, pool, orgID, clusterUUID, namespace)
}

// Re-export pgx.ErrNoRows for callers that compare delete outcomes.
var ErrNoRows = pgx.ErrNoRows
