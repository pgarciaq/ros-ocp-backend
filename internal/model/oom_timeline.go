package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OOMTimelineEntry represents a single day with OOM kill events.
type OOMTimelineEntry struct {
	Date         string `json:"date"`
	OOMKillCount int64  `json:"oom_kill_count"`
}

// OOMTimelineMeta holds response metadata for the OOM timeline endpoint.
type OOMTimelineMeta struct {
	Count       int64  `json:"count"`
	ContainerID string `json:"container_id"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
}

// OOMTimelineResponse is the top-level response for the OOM timeline endpoint.
type OOMTimelineResponse struct {
	Meta OOMTimelineMeta    `json:"meta"`
	Data []OOMTimelineEntry `json:"data"`
}

// ResolveContainerKeyByID looks up the container composite key from
// recommendation_sets using the deterministic container_id UUID.
// Returns nil if not found or the org doesn't match.
func ResolveContainerKeyByID(ctx context.Context, pool *pgxpool.Pool, orgID, containerID string) (*ContainerKey, error) {
	var key ContainerKey
	err := pool.QueryRow(ctx, `
		SELECT cluster_uuid, namespace, workload, workload_type, container_name
		FROM recommendation_sets
		WHERE org_id = $1 AND container_id = $2
		LIMIT 1`,
		orgID, containerID,
	).Scan(&key.ClusterUUID, &key.Namespace, &key.Workload, &key.WorkloadType, &key.ContainerName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("ResolveContainerKeyByID: %w", err)
	}
	key.OrgID = orgID
	return &key, nil
}

// QueryOOMTimeline returns days with non-zero OOM events for a container,
// scoped by org_id and an optional date range.
func QueryOOMTimeline(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	key ContainerKey,
	startDate, endDate time.Time,
) ([]OOMTimelineEntry, error) {
	rows, err := pool.Query(ctx, `
		SELECT bucket_date, oom_count_sum
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2
			AND namespace = $3 AND workload = $4
			AND workload_type = $5 AND container_name = $6
			AND bucket_date >= $7 AND bucket_date <= $8
			AND oom_count_sum > 0
			AND schedule_type = 'all_hours'
		ORDER BY bucket_date ASC`,
		key.OrgID, key.ClusterUUID, key.Namespace, key.Workload,
		key.WorkloadType, key.ContainerName,
		startDate, endDate,
	)
	if err != nil {
		return nil, fmt.Errorf("QueryOOMTimeline query: %w", err)
	}
	defer rows.Close()

	var entries []OOMTimelineEntry
	for rows.Next() {
		var bucketDate time.Time
		var oomCount int64
		if err := rows.Scan(&bucketDate, &oomCount); err != nil {
			return nil, fmt.Errorf("QueryOOMTimeline scan: %w", err)
		}
		entries = append(entries, OOMTimelineEntry{
			Date:         bucketDate.Format("2006-01-02"),
			OOMKillCount: oomCount,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("QueryOOMTimeline rows: %w", err)
	}

	if entries == nil {
		entries = []OOMTimelineEntry{}
	}
	return entries, nil
}
