package bhschedule

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UpsertSchedule inserts or updates a schedule row at the given scope.
func UpsertSchedule(ctx context.Context, pool *pgxpool.Pool, sched Schedule) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO business_hours_schedules (
			org_id, cluster_uuid, namespace, timezone, days, start_time, end_time,
			off_hours_weight, enabled, updated_at
		) VALUES ($1, $2::uuid, $3, $4, $5, $6::time, $7::time, $8, $9, NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace)
		DO UPDATE SET
			timezone = EXCLUDED.timezone,
			days = EXCLUDED.days,
			start_time = EXCLUDED.start_time,
			end_time = EXCLUDED.end_time,
			off_hours_weight = EXCLUDED.off_hours_weight,
			enabled = EXCLUDED.enabled,
			updated_at = NOW()`,
		sched.OrgID, sched.ClusterUUID, sched.Namespace, sched.Timezone, sched.Days,
		sched.StartTime, sched.EndTime, sched.OffHoursWeight, sched.Enabled,
	)
	if err != nil {
		return fmt.Errorf("upserting business hours schedule: %w", err)
	}
	return nil
}

// DeleteSchedule removes a schedule row at the given scope.
func DeleteSchedule(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, namespace string) error {
	tag, err := pool.Exec(ctx, `
		DELETE FROM business_hours_schedules
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3`,
		orgID, clusterUUID, namespace,
	)
	if err != nil {
		return fmt.Errorf("deleting business hours schedule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func normalizeDays(days []string) []string {
	out := make([]string, len(days))
	for i, d := range days {
		out[i] = strings.ToLower(strings.TrimSpace(d))
	}
	return out
}
