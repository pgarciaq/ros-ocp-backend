package engine

import (
	"context"
	"fmt"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// NodeGPUTimeslicingHistoryOrderBy maps API order_by keys to SQL column names.
var NodeGPUTimeslicingHistoryOrderBy = map[string]string{
	"recorded_at":          "recorded_at",
	"recommended_replicas": "recommended_replicas",
	"confidence":           "confidence",
	"candidate_count":      "candidate_count",
	"impacted_count":       "impacted_count",
}

// ListNodeGPUTimeslicingRecommendationHistory returns paginated history snapshots
// for a node GPU time-slicing recommendation key.
func ListNodeGPUTimeslicingRecommendationHistory(
	ctx context.Context,
	q db.QueryRower,
	orgID, clusterUUID, nodeName, gpuModel, term string,
	orderCol, orderDir string,
	limit, offset int,
) ([]model.NodeGPUTimeslicingRecommendationHistory, int64, error) {
	if q == nil {
		return nil, 0, fmt.Errorf("database pool unavailable")
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	if orderCol == "" {
		orderCol = "recorded_at"
	}
	if orderDir != "asc" && orderDir != "desc" {
		orderDir = "desc"
	}

	baseWhere := `
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND node_name = $3`
	args := []any{orgID, clusterUUID, nodeName}
	argIdx := 4

	if gpuModel != "" {
		baseWhere += fmt.Sprintf(" AND gpu_model = $%d", argIdx)
		args = append(args, gpuModel)
		argIdx++
	}
	if term != "" {
		baseWhere += fmt.Sprintf(" AND term = $%d", argIdx)
		args = append(args, term)
		argIdx++
	}

	var total int64
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM node_gpu_timeslicing_recommendation_history`+baseWhere, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count node GPU time-slicing history: %w", err)
	}

	query := `
		SELECT id, org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, candidate_count, impacted_count,
			estimated_savings_cents, recorded_at
		FROM node_gpu_timeslicing_recommendation_history` + baseWhere +
		fmt.Sprintf(" ORDER BY %s %s, id DESC LIMIT $%d OFFSET $%d", orderCol, orderDir, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list node GPU time-slicing history: %w", err)
	}
	defer rows.Close()

	var result []model.NodeGPUTimeslicingRecommendationHistory
	for rows.Next() {
		var row model.NodeGPUTimeslicingRecommendationHistory
		if scanErr := rows.Scan(
			&row.ID, &row.OrgID, &row.ClusterUUID, &row.NodeName, &row.GPUModel, &row.Term,
			&row.RecommendedReplicas, &row.Confidence, &row.CandidateCount, &row.ImpactedCount,
			&row.EstimatedSavingsCents, &row.RecordedAt,
		); scanErr != nil {
			return nil, 0, fmt.Errorf("scan node GPU time-slicing history: %w", scanErr)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return result, total, nil
}
