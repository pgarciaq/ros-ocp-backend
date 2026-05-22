package bhschedule

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OrgClusterSentinelUUID is the cluster_uuid value for org-wide default rows.
const OrgClusterSentinelUUID = "00000000-0000-0000-0000-000000000000"

// Cache holds schedules for one ingestion batch (org + cluster).
type Cache struct {
	orgID       string
	clusterUUID string
	namespace   map[string]Schedule
	cluster     *Schedule
	org         *Schedule
}

// LoadSchedules loads all schedule rows for orgID and clusterUUID (including org default).
func LoadSchedules(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (*Cache, error) {
	rows, err := pool.Query(ctx, `
		SELECT cluster_uuid::text, namespace, timezone, days,
			to_char(start_time, 'HH24:MI') AS start_time,
			to_char(end_time, 'HH24:MI') AS end_time,
			off_hours_weight, enabled
		FROM business_hours_schedules
		WHERE org_id = $1
		  AND (cluster_uuid = $2::uuid OR cluster_uuid = $3::uuid)`,
		orgID, clusterUUID, OrgClusterSentinelUUID,
	)
	if err != nil {
		return nil, fmt.Errorf("loading business hours schedules: %w", err)
	}
	defer rows.Close()

	cache := &Cache{
		orgID:       orgID,
		clusterUUID: clusterUUID,
		namespace:   make(map[string]Schedule),
	}

	for rows.Next() {
		var (
			rowClusterUUID string
			namespace      string
			timezone       string
			days           []string
			startTime      string
			endTime        string
			offHoursWeight float32
			enabled        bool
		)
		if err := rows.Scan(
			&rowClusterUUID, &namespace, &timezone, &days,
			&startTime, &endTime, &offHoursWeight, &enabled,
		); err != nil {
			return nil, fmt.Errorf("scanning business hours schedule: %w", err)
		}

		sched := Schedule{
			OrgID:          orgID,
			ClusterUUID:    rowClusterUUID,
			Namespace:      namespace,
			Timezone:       timezone,
			Days:           normalizeDays(days),
			StartTime:      startTime,
			EndTime:        endTime,
			OffHoursWeight: float64(offHoursWeight),
			Enabled:        enabled,
		}

		switch {
		case rowClusterUUID == OrgClusterSentinelUUID && namespace == "":
			s := sched
			cache.org = &s
		case rowClusterUUID == clusterUUID && namespace == "":
			s := sched
			cache.cluster = &s
		case rowClusterUUID == clusterUUID && namespace != "":
			cache.namespace[namespace] = sched
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating business hours schedules: %w", err)
	}

	return cache, nil
}

// HasAnyEnabled reports whether any org, cluster, or namespace schedule row is enabled.
func (c *Cache) HasAnyEnabled() bool {
	if c == nil {
		return false
	}
	if c.org != nil && c.org.Enabled {
		return true
	}
	if c.cluster != nil && c.cluster.Enabled {
		return true
	}
	for _, row := range c.namespace {
		if row.Enabled {
			return true
		}
	}
	return false
}

// Resolve returns the effective schedule for namespace using inheritance.
func (c *Cache) Resolve(namespace string) Schedule {
	if c == nil {
		return AllHoursSchedule()
	}
	if row, ok := c.namespace[namespace]; ok {
		return row
	}
	if c.cluster != nil {
		return *c.cluster
	}
	if c.org != nil {
		return *c.org
	}
	return AllHoursSchedule()
}
