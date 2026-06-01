package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const quotaRecHistoryRetentionDays = 90

// QuotaRecommendationHistoryRow is one historical namespace quota recommendation snapshot.
type QuotaRecommendationHistoryRow struct {
	RecordedAt           time.Time `json:"recorded_at"`
	RecommendationType   string    `json:"recommendation_type"`
	RiskLevel            string    `json:"risk_level"`
	CPURequestHardMC     *int64    `json:"cpu_request_hard_millicores,omitempty"`
	CPURequestUsedMC     *int64    `json:"cpu_request_used_millicores,omitempty"`
	CPURequestRecommendedMC *int64 `json:"cpu_request_recommended_millicores,omitempty"`
	MemoryRequestHardBytes *int64  `json:"memory_request_hard_bytes,omitempty"`
	MemoryRequestUsedBytes *int64  `json:"memory_request_used_bytes,omitempty"`
	MemoryRequestRecommendedBytes *int64 `json:"memory_request_recommended_bytes,omitempty"`
	MaxUtilizationPercent *float64 `json:"max_utilization_percent,omitempty"`
}

// AppendQuotaRecommendationHistory inserts a snapshot after each quota upsert.
func AppendQuotaRecommendationHistory(ctx context.Context, pool *pgxpool.Pool, recs []QuotaRec) error {
	if len(recs) == 0 {
		return nil
	}
	for _, r := range recs {
		maxUtil := maxUtilizationPercent(r.Utilization)
		_, err := pool.Exec(ctx, `
			INSERT INTO quota_recommendation_history (
				org_id, cluster_uuid, namespace,
				recommendation_type, risk_level,
				cpu_request_hard_millicores, cpu_request_used_millicores,
				cpu_request_recommended_millicores,
				memory_request_hard_bytes, memory_request_used_bytes,
				memory_request_recommended_bytes,
				max_utilization_percent
			) VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			r.OrgID, r.ClusterUUID, r.Namespace,
			r.RecommendationType, r.RiskLevel,
			nullableInt64(r.Snapshot.CPURequestHardMC), nullableInt64(r.Snapshot.CPURequestUsedMC),
			nullableInt64(r.Recommended.CPURequestMillicores),
			nullableInt64(r.Snapshot.MemoryRequestHardBytes), nullableInt64(r.Snapshot.MemoryRequestUsedBytes),
			nullableInt64(r.Recommended.MemoryRequestBytes),
			maxUtil,
		)
		if err != nil {
			return fmt.Errorf("insert quota rec history %s: %w", r.Namespace, err)
		}
	}
	return nil
}

// PruneQuotaRecommendationHistory deletes rows older than the retention window.
func PruneQuotaRecommendationHistory(ctx context.Context, pool *pgxpool.Pool) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -quotaRecHistoryRetentionDays)
	_, err := pool.Exec(ctx, `DELETE FROM quota_recommendation_history WHERE recorded_at < $1`, cutoff)
	if err != nil {
		return fmt.Errorf("prune quota rec history: %w", err)
	}
	return nil
}

// ListQuotaRecommendationHistory returns historical snapshots for a namespace quota.
func ListQuotaRecommendationHistory(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, namespace string,
	limit int,
) ([]QuotaRecommendationHistoryRow, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 90 {
		limit = 90
	}
	rows, err := pool.Query(ctx, `
		SELECT recorded_at, recommendation_type, risk_level,
			cpu_request_hard_millicores, cpu_request_used_millicores,
			cpu_request_recommended_millicores,
			memory_request_hard_bytes, memory_request_used_bytes,
			memory_request_recommended_bytes,
			max_utilization_percent
		FROM quota_recommendation_history
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3
		ORDER BY recorded_at DESC
		LIMIT $4`,
		orgID, clusterUUID, namespace, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list quota rec history: %w", err)
	}
	defer rows.Close()

	var out []QuotaRecommendationHistoryRow
	for rows.Next() {
		var row QuotaRecommendationHistoryRow
		var cpuHard, cpuUsed, cpuRec, memHard, memUsed, memRec *int64
		var maxUtil *float64
		if err := rows.Scan(
			&row.RecordedAt, &row.RecommendationType, &row.RiskLevel,
			&cpuHard, &cpuUsed, &cpuRec,
			&memHard, &memUsed, &memRec,
			&maxUtil,
		); err != nil {
			return nil, fmt.Errorf("scan quota rec history: %w", err)
		}
		row.CPURequestHardMC = cpuHard
		row.CPURequestUsedMC = cpuUsed
		row.CPURequestRecommendedMC = cpuRec
		row.MemoryRequestHardBytes = memHard
		row.MemoryRequestUsedBytes = memUsed
		row.MemoryRequestRecommendedBytes = memRec
		row.MaxUtilizationPercent = maxUtil
		out = append(out, row)
	}
	return out, rows.Err()
}

func maxUtilizationPercent(util QuotaUtilizationBP) *float64 {
	maxBP := maxUtilizationBP(util)
	if maxBP <= 0 {
		return nil
	}
	pct := float64(maxBP) / 100.0
	return &pct
}
