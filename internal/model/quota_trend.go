package model

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// QuotaTrendEntry represents a single day's quota hard vs used values.
type QuotaTrendEntry struct {
	Date                     string `json:"date"`
	CPURequestHardMillicores *int64 `json:"cpu_request_hard_millicores"`
	CPURequestUsedMillicores *int64 `json:"cpu_request_used_millicores"`
	MemoryRequestHardBytes   *int64 `json:"memory_request_hard_bytes"`
	MemoryRequestUsedBytes   *int64 `json:"memory_request_used_bytes"`
}

// QuotaTrendMeta holds response metadata for the quota trend endpoint.
type QuotaTrendMeta struct {
	Count       int    `json:"count"`
	ClusterUUID string `json:"cluster_uuid"`
	Namespace   string `json:"namespace"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
}

// QuotaTrendResponse is the top-level response for the quota trend endpoint.
type QuotaTrendResponse struct {
	Meta QuotaTrendMeta    `json:"meta"`
	Data []QuotaTrendEntry `json:"data"`
}

// QuotaIdentity holds the resolved composite key for a quota recommendation.
type QuotaIdentity struct {
	ClusterUUID string
	Namespace   string
	QuotaName   string
}

// ResolveQuotaKeyByID resolves the composite key (cluster_uuid, namespace, quota_name)
// for a quota recommendation set using its deterministic UUID v5 ID.
// Returns nil if not found or the org doesn't match.
//
// Primary path: O(1) indexed lookup on the persisted quota_id column.
// Fallback path: scans rows where quota_id IS NULL (pre-backfill), which
// self-heals after one reconcile cycle populates the column via UPSERT.
func ResolveQuotaKeyByID(ctx context.Context, pool *pgxpool.Pool, orgID, quotaID string) (*QuotaIdentity, error) {
	var cu, ns, qn string
	err := pool.QueryRow(ctx, `
		SELECT cluster_uuid::text, namespace, quota_name
		FROM quota_recommendation_sets
		WHERE org_id = $1 AND quota_id = $2
		LIMIT 1`, orgID, quotaID).Scan(&cu, &ns, &qn)
	if err == nil {
		return &QuotaIdentity{ClusterUUID: cu, Namespace: ns, QuotaName: qn}, nil
	}
	if err != pgx.ErrNoRows {
		return nil, fmt.Errorf("ResolveQuotaKeyByID query: %w", err)
	}

	// Fallback: scan rows with NULL quota_id (pre-backfill).
	var matched *QuotaIdentity
	fErr := database.WithHeavyStatementTimeout(ctx, pool, func(ctx context.Context, q database.QueryRower) error {
		rows, qErr := q.Query(ctx, `
			SELECT cluster_uuid::text, namespace, quota_name
			FROM quota_recommendation_sets
			WHERE org_id = $1 AND quota_id IS NULL`, orgID)
		if qErr != nil {
			return fmt.Errorf("ResolveQuotaKeyByID fallback query: %w", qErr)
		}
		defer rows.Close()
		for rows.Next() {
			if sErr := rows.Scan(&cu, &ns, &qn); sErr != nil {
				return fmt.Errorf("ResolveQuotaKeyByID fallback scan: %w", sErr)
			}
			if NativeQuotaID(cu, ns, qn) == quotaID {
				matched = &QuotaIdentity{ClusterUUID: cu, Namespace: ns, QuotaName: qn}
				return nil
			}
		}
		if rErr := rows.Err(); rErr != nil {
			return fmt.Errorf("ResolveQuotaKeyByID fallback rows: %w", rErr)
		}
		return nil
	})
	if fErr != nil {
		database.RecordStatementTimeoutCancellation(fErr)
		return nil, fErr
	}
	if matched != nil {
		return matched, nil
	}

	// Retry indexed path: a concurrent UPSERT may have populated quota_id
	// between our first indexed lookup and the fallback scan, moving the row
	// out of the NULL set (TOCTOU window during backfill transition).
	err = pool.QueryRow(ctx, `
		SELECT cluster_uuid::text, namespace, quota_name
		FROM quota_recommendation_sets
		WHERE org_id = $1 AND quota_id = $2
		LIMIT 1`, orgID, quotaID).Scan(&cu, &ns, &qn)
	if err == nil {
		return &QuotaIdentity{ClusterUUID: cu, Namespace: ns, QuotaName: qn}, nil
	}
	if err != pgx.ErrNoRows {
		return nil, fmt.Errorf("ResolveQuotaKeyByID retry: %w", err)
	}
	return nil, nil
}

// QueryQuotaTrend returns daily quota hard vs used values for a namespace,
// scoped by org_id and a date range.
func QueryQuotaTrend(
	ctx context.Context,
	q database.QueryRower,
	orgID, clusterUUID, namespace string,
	startDate, endDate time.Time,
) ([]QuotaTrendEntry, error) {
	rows, err := q.Query(ctx, `
		SELECT report_date,
			cpu_request_hard,
			cpu_request_used,
			memory_request_hard,
			memory_request_used
		FROM daily_namespace_quota_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3
			AND report_date >= $4 AND report_date <= $5
		ORDER BY report_date ASC`,
		orgID, clusterUUID, namespace, startDate, endDate,
	)
	if err != nil {
		return nil, fmt.Errorf("QueryQuotaTrend query: %w", err)
	}
	defer rows.Close()

	var entries []QuotaTrendEntry
	for rows.Next() {
		var reportDate time.Time
		var cpuHard, cpuUsed, memHard, memUsed *int64
		if err := rows.Scan(&reportDate, &cpuHard, &cpuUsed, &memHard, &memUsed); err != nil {
			return nil, fmt.Errorf("QueryQuotaTrend scan: %w", err)
		}
		entries = append(entries, QuotaTrendEntry{
			Date:                     reportDate.Format("2006-01-02"),
			CPURequestHardMillicores: cpuHard,
			CPURequestUsedMillicores: cpuUsed,
			MemoryRequestHardBytes:   memHard,
			MemoryRequestUsedBytes:   memUsed,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("QueryQuotaTrend rows: %w", err)
	}

	if entries == nil {
		entries = []QuotaTrendEntry{}
	}
	return entries, nil
}
