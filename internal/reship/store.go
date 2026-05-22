package reship

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
)

// PendingCluster identifies a cluster with a pending masu reship.
type PendingCluster struct {
	OrgID        string
	ClusterUUID  uuid.UUID
	PendingSince time.Time
}

// MarkReshipPending sets reship_pending_since for rows matching the cluster (and org default fallback).
func MarkReshipPending(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID) error {
	tag, err := pool.Exec(ctx, `
		UPDATE business_hours_schedules
		SET reship_pending_since = NOW()
		WHERE org_id = $1
		  AND (
		    cluster_uuid = $2::uuid
		    OR (cluster_uuid = $3::uuid AND NOT EXISTS (
		      SELECT 1 FROM business_hours_schedules
		      WHERE org_id = $1 AND cluster_uuid = $2::uuid
		    ))
		  )`,
		orgID, clusterUUID.String(), bhschedule.OrgClusterSentinelUUID,
	)
	if err != nil {
		return fmt.Errorf("mark reship pending: %w", err)
	}
	if tag.RowsAffected() == 0 {
		_, err = pool.Exec(ctx, `
			INSERT INTO business_hours_schedules (
				org_id, cluster_uuid, namespace, timezone, days, start_time, end_time,
				off_hours_weight, enabled, reship_pending_since, updated_at
			) VALUES (
				$1, $2::uuid, '', 'UTC', ARRAY['monday'], '00:00', '23:59',
				0.0, false, NOW(), NOW()
			)
			ON CONFLICT (org_id, cluster_uuid, namespace)
			DO UPDATE SET reship_pending_since = NOW()`,
			orgID, clusterUUID.String(),
		)
		if err != nil {
			return fmt.Errorf("insert reship pending marker: %w", err)
		}
	}
	return nil
}

// ClearReshipPending clears reship_pending_since for the cluster scope.
func ClearReshipPending(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID) error {
	_, err := pool.Exec(ctx, `
		UPDATE business_hours_schedules
		SET reship_pending_since = NULL
		WHERE org_id = $1
		  AND (
		    cluster_uuid = $2::uuid
		    OR cluster_uuid = $3::uuid
		  )`,
		orgID, clusterUUID.String(), bhschedule.OrgClusterSentinelUUID,
	)
	if err != nil {
		return fmt.Errorf("clear reship pending: %w", err)
	}
	return nil
}

// ListPendingClusters returns distinct clusters with a non-null reship_pending_since.
func ListPendingClusters(ctx context.Context, pool *pgxpool.Pool) ([]PendingCluster, error) {
	rows, err := pool.Query(ctx, `
		SELECT org_id, cluster_uuid, MIN(reship_pending_since)
		FROM business_hours_schedules
		WHERE reship_pending_since IS NOT NULL
		  AND cluster_uuid <> $1::uuid
		GROUP BY org_id, cluster_uuid
		ORDER BY MIN(reship_pending_since)`,
		bhschedule.OrgClusterSentinelUUID,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending reships: %w", err)
	}
	defer rows.Close()

	var out []PendingCluster
	for rows.Next() {
		var pc PendingCluster
		if err := rows.Scan(&pc.OrgID, &pc.ClusterUUID, &pc.PendingSince); err != nil {
			return nil, fmt.Errorf("scan pending reship: %w", err)
		}
		out = append(out, pc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// MaxScheduleUpdatedAt returns the latest updated_at for schedule rows at cluster scope.
func MaxScheduleUpdatedAt(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID) (time.Time, error) {
	var ts time.Time
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(updated_at), 'epoch'::timestamptz)
		FROM business_hours_schedules
		WHERE org_id = $1
		  AND (cluster_uuid = $2::uuid OR cluster_uuid = $3::uuid)`,
		orgID, clusterUUID.String(), bhschedule.OrgClusterSentinelUUID,
	).Scan(&ts)
	if err != nil {
		if errorsIsNoRows(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("max schedule updated_at: %w", err)
	}
	return ts.UTC(), nil
}

// ReshipPendingSince returns whether pending is set for the cluster.
func ReshipPendingSince(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID) (*time.Time, error) {
	var ts *time.Time
	err := pool.QueryRow(ctx, `
		SELECT reship_pending_since
		FROM business_hours_schedules
		WHERE org_id = $1
		  AND cluster_uuid = $2::uuid
		  AND reship_pending_since IS NOT NULL
		LIMIT 1`,
		orgID, clusterUUID.String(),
	).Scan(&ts)
	if err != nil {
		if errorsIsNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reship pending since: %w", err)
	}
	return ts, nil
}

func errorsIsNoRows(err error) bool {
	return err == pgx.ErrNoRows
}
