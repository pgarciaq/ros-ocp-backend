package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const quotaRecHistoryRetentionDays = 90

// QuotaRecommendationHistoryRow is one historical namespace quota resource snapshot.
type QuotaRecommendationHistoryRow struct {
	RecordedAt         time.Time `json:"recorded_at"`
	Resource           string    `json:"resource"`
	RecommendationType string    `json:"recommendation_type"`
	RiskLevel          string    `json:"risk_level"`
	RecommendedHard    *int64    `json:"recommended_hard,omitempty"`
	CurrentHard        *int64    `json:"current_hard,omitempty"`
	CurrentUsed        *int64    `json:"current_used,omitempty"`
	UtilizationPercent *int      `json:"utilization_percent,omitempty"`
}

// AppendQuotaRecommendationHistory inserts per-resource snapshots after each quota upsert.
func AppendQuotaRecommendationHistory(ctx context.Context, pool *pgxpool.Pool, recs []QuotaRec) error {
	if len(recs) == 0 {
		return nil
	}
	for _, r := range recs {
		for _, entry := range quotaHistoryEntries(r) {
			_, err := pool.Exec(ctx, `
				INSERT INTO quota_recommendation_history (
					org_id, cluster_uuid, namespace, quota_name,
					resource, recommendation_type, risk_level,
					recommended_hard, current_hard, current_used, utilization_percent
				) VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				r.OrgID, r.ClusterUUID, r.Namespace, r.QuotaName,
				entry.resource, r.RecommendationType, r.RiskLevel,
				nullableInt64(entry.recommendedHard), nullableInt64(entry.currentHard),
				nullableInt64(entry.currentUsed), entry.utilizationPercent,
			)
			if err != nil {
				return fmt.Errorf("insert quota rec history %s/%s: %w", r.Namespace, entry.resource, err)
			}
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
	orgID, clusterUUID, namespace, quotaName string,
	limit int,
) ([]QuotaRecommendationHistoryRow, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 90 {
		limit = 90
	}
	rows, err := pool.Query(ctx, `
		SELECT recorded_at, resource, recommendation_type, risk_level,
			recommended_hard, current_hard, current_used, utilization_percent
		FROM quota_recommendation_history
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3 AND quota_name = $4
			AND resource IS NOT NULL AND resource <> ''
		ORDER BY recorded_at DESC, resource
		LIMIT $5`,
		orgID, clusterUUID, namespace, quotaName, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list quota rec history: %w", err)
	}
	defer rows.Close()

	var out []QuotaRecommendationHistoryRow
	for rows.Next() {
		var row QuotaRecommendationHistoryRow
		var recHard, curHard, curUsed *int64
		var util *int
		if err := rows.Scan(
			&row.RecordedAt, &row.Resource, &row.RecommendationType, &row.RiskLevel,
			&recHard, &curHard, &curUsed, &util,
		); err != nil {
			return nil, fmt.Errorf("scan quota rec history: %w", err)
		}
		row.RecommendedHard = recHard
		row.CurrentHard = curHard
		row.CurrentUsed = curUsed
		row.UtilizationPercent = util
		out = append(out, row)
	}
	return out, rows.Err()
}

type quotaHistoryEntry struct {
	resource           string
	recommendedHard    int64
	currentHard        int64
	currentUsed        int64
	utilizationPercent *int
}

func quotaHistoryEntries(r QuotaRec) []quotaHistoryEntry {
	s := r.Snapshot
	var entries []quotaHistoryEntry
	add := func(resource string, hard, used, recommended int64, util *int) {
		if hard <= 0 {
			return
		}
		entries = append(entries, quotaHistoryEntry{
			resource:           resource,
			recommendedHard:    recommended,
			currentHard:        hard,
			currentUsed:        used,
			utilizationPercent: bpToPercentInt(util),
		})
	}
	add("cpu_request", s.CPURequestHardMC, s.CPURequestUsedMC, r.Recommended.CPURequestMillicores, r.Utilization.CPURequestBP)
	add("memory_request", s.MemoryRequestHardBytes, s.MemoryRequestUsedBytes, r.Recommended.MemoryRequestBytes, r.Utilization.MemoryRequestBP)
	add("storage_request", s.StorageRequestHardBytes, s.StorageRequestUsedBytes, r.Recommended.StorageRequestBytes, r.Utilization.StorageRequestBP)
	add("pods", s.PodsHard, s.PodsUsed, r.Recommended.Pods, r.Utilization.PodsBP)
	return entries
}
