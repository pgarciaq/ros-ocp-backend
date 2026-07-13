package quota

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

const clusterQuotaRecHistoryRetentionDays = 90

// ClusterQuotaRecommendationHistoryRow is one historical CRQ resource snapshot.
type ClusterQuotaRecommendationHistoryRow struct {
	RecordedAt         time.Time `json:"recorded_at"`
	Resource           string    `json:"resource"`
	RecommendationType string    `json:"recommendation_type"`
	RiskLevel          string    `json:"risk_level"`
	RecommendedHard    *int64    `json:"recommended_hard,omitempty"`
	CurrentHard        *int64    `json:"current_hard,omitempty"`
	CurrentUsed        *int64    `json:"current_used,omitempty"`
	UtilizationPercent *int      `json:"utilization_percent,omitempty"`
}

// AppendClusterQuotaRecommendationHistory inserts per-resource snapshots after each CRQ upsert.
func AppendClusterQuotaRecommendationHistory(ctx context.Context, pool *pgxpool.Pool, recs []ClusterQuotaRec) error {
	if len(recs) == 0 {
		return nil
	}
	for _, r := range recs {
		for _, entry := range clusterQuotaHistoryEntries(r) {
			_, err := pool.Exec(ctx, `
				INSERT INTO cluster_quota_recommendation_history (
					org_id, cluster_uuid, cluster_quota_name,
					resource, recommendation_type, risk_level,
					recommended_hard, current_hard, current_used, utilization_percent,`+core.ClusterQuotaExplSQLColumns+`
				) VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
				append([]any{
					r.OrgID, r.ClusterUUID, r.ClusterQuotaName,
					entry.resource, r.RecommendationType, r.RiskLevel,
					nullableInt64(entry.recommendedHard), nullableInt64(entry.currentHard),
					nullableInt64(entry.currentUsed), entry.utilizationPercent,
				}, core.AppendClusterQuotaExplArgs(nil, r.Expl)...)...,
			)
			if err != nil {
				return fmt.Errorf("insert cluster quota rec history %s/%s: %w", r.ClusterQuotaName, entry.resource, err)
			}
		}
	}
	return nil
}

// PruneClusterQuotaRecommendationHistory deletes rows older than the retention window.
func PruneClusterQuotaRecommendationHistory(ctx context.Context, pool *pgxpool.Pool) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -clusterQuotaRecHistoryRetentionDays)
	_, err := pool.Exec(ctx, `DELETE FROM cluster_quota_recommendation_history WHERE recorded_at < $1`, cutoff)
	if err != nil {
		return fmt.Errorf("prune cluster quota rec history: %w", err)
	}
	return nil
}

// ListClusterQuotaRecommendationHistory returns historical snapshots for one CRQ.
func ListClusterQuotaRecommendationHistory(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, clusterQuotaName string,
	limit int,
) ([]ClusterQuotaRecommendationHistoryRow, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 90 {
		limit = 90
	}
	rows, err := pool.Query(ctx, `
		SELECT recorded_at, resource, recommendation_type, risk_level,
			recommended_hard, current_hard, current_used, utilization_percent
		FROM cluster_quota_recommendation_history
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND cluster_quota_name = $3
		ORDER BY recorded_at DESC, resource
		LIMIT $4`,
		orgID, clusterUUID, clusterQuotaName, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list cluster quota rec history: %w", err)
	}
	defer rows.Close()

	var out []ClusterQuotaRecommendationHistoryRow
	for rows.Next() {
		var row ClusterQuotaRecommendationHistoryRow
		var recHard, curHard, curUsed *int64
		var util *int
		if err := rows.Scan(
			&row.RecordedAt, &row.Resource, &row.RecommendationType, &row.RiskLevel,
			&recHard, &curHard, &curUsed, &util,
		); err != nil {
			return nil, fmt.Errorf("scan cluster quota rec history: %w", err)
		}
		row.RecommendedHard = recHard
		row.CurrentHard = curHard
		row.CurrentUsed = curUsed
		row.UtilizationPercent = util
		out = append(out, row)
	}
	return out, rows.Err()
}

type clusterQuotaHistoryEntry struct {
	resource            string
	recommendedHard     int64
	currentHard         int64
	currentUsed         int64
	utilizationPercent  *int
}

func clusterQuotaHistoryEntries(r ClusterQuotaRec) []clusterQuotaHistoryEntry {
	s := r.Snapshot
	var entries []clusterQuotaHistoryEntry
	add := func(resource string, hard, used, recommended int64, util *int) {
		if hard <= 0 {
			return
		}
		entries = append(entries, clusterQuotaHistoryEntry{
			resource:           resource,
			recommendedHard:    recommended,
			currentHard:        hard,
			currentUsed:        used,
			utilizationPercent: util,
		})
	}
	add("cpu_request", s.CPURequestHardMC, s.CPURequestUsedMC, r.Recommended.CPURequestMillicores, r.UtilizationCPURequestPercent)
	add("memory_request", s.MemoryRequestHardBytes, s.MemoryRequestUsedBytes, r.Recommended.MemoryRequestBytes, r.UtilizationMemoryRequestPercent)
	add("storage_request", s.StorageRequestHardBytes, s.StorageRequestUsedBytes, r.StorageRecommendedBytes, r.UtilizationStorageRequestPercent)
	add("pods", s.PodsHard, s.PodsUsed, r.PodsRecommended, r.UtilizationPodsPercent)
	return entries
}
