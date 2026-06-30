package model

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
func ResolveQuotaKeyByID(ctx context.Context, pool *pgxpool.Pool, orgID, quotaID string) (*QuotaIdentity, error) {
	rows, err := pool.Query(ctx, `
		SELECT cluster_uuid::text, namespace, quota_name
		FROM quota_recommendation_sets
		WHERE org_id = $1`, orgID)
	if err != nil {
		return nil, fmt.Errorf("ResolveQuotaKeyByID query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cu, ns, qn string
		if sErr := rows.Scan(&cu, &ns, &qn); sErr != nil {
			return nil, fmt.Errorf("ResolveQuotaKeyByID scan: %w", sErr)
		}
		if NativeQuotaID(cu, ns, qn) == quotaID {
			return &QuotaIdentity{ClusterUUID: cu, Namespace: ns, QuotaName: qn}, nil
		}
	}
	if rErr := rows.Err(); rErr != nil {
		return nil, fmt.Errorf("ResolveQuotaKeyByID rows: %w", rErr)
	}
	return nil, nil
}

// QueryQuotaTrend returns daily quota hard vs used values for a namespace,
// scoped by org_id and a date range.
func QueryQuotaTrend(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, namespace string,
	startDate, endDate time.Time,
) ([]QuotaTrendEntry, error) {
	rows, err := pool.Query(ctx, `
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
