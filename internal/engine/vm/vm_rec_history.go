package vm

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// VMRecommendationHistoryRow is one historical VM recommendation snapshot.
type VMRecommendationHistoryRow struct {
	ID                      int64     `json:"id"`
	OrgID                   string    `json:"org_id"`
	ClusterID               string    `json:"cluster_id"`
	VMName                  string    `json:"vm_name"`
	Namespace               string    `json:"namespace"`
	Term                    string    `json:"term"`
	Engine                  string    `json:"engine"`
	RecommendedVCPU         int32     `json:"recommended_vcpu"`
	RecommendedMemoryGiB    float64   `json:"recommended_memory_gib"`
	RecommendedInstanceType string    `json:"recommended_instance_type"`
	GPUClassification       string    `json:"gpu_classification"`
	RecommendedGPUAction    string    `json:"recommended_gpu_action"`
	Category                string    `json:"category"`
	Confidence              string    `json:"confidence"`
	CreatedAt               time.Time `json:"created_at"`
}

// AppendVMRecommendationHistory inserts history snapshots inside the caller's transaction.
func AppendVMRecommendationHistory(ctx context.Context, tx pgx.Tx, recs []Recommendation) error {
	return pgrec.AppendVMRecommendationHistory(ctx, tx, recs)
}

// PruneVMRecommendationHistory deletes rows older than the configured retention window.
func PruneVMRecommendationHistory(ctx context.Context, pool *pgxpool.Pool) error {
	cfg := config.GetConfig()
	days := cfg.VMRecHistoryRetentionDays
	if days <= 0 {
		days = 90
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	tag, err := pool.Exec(ctx, `DELETE FROM vm_recommendation_history WHERE created_at < $1`, cutoff)
	if err != nil {
		return fmt.Errorf("prune VM rec history: %w", err)
	}
	if tag.RowsAffected() > 0 {
		logging.GetLogger().Infof("PruneVMRecommendationHistory: deleted %d rows older than %d days", tag.RowsAffected(), days)
	}
	return nil
}

// ListVMRecommendationHistory returns historical snapshots for a VM.
func ListVMRecommendationHistory(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterID, vmName, namespace, term, engine string,
	limit, offset int,
) ([]VMRecommendationHistoryRow, int64, error) {
	if pool == nil {
		return nil, 0, fmt.Errorf("database pool unavailable")
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM vm_recommendation_history
		WHERE org_id = $1 AND cluster_id = $2 AND vm_name = $3 AND namespace = $4
		  AND term = $5 AND engine = $6`,
		orgID, clusterID, vmName, namespace, term, engine,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count VM rec history: %w", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT id, org_id, cluster_id, vm_name, namespace, term, engine,
			recommended_vcpu, recommended_memory_gib, recommended_instance_type,
			gpu_classification, recommended_gpu_action,
			category, confidence, created_at
		FROM vm_recommendation_history
		WHERE org_id = $1 AND cluster_id = $2 AND vm_name = $3 AND namespace = $4
		  AND term = $5 AND engine = $6
		ORDER BY created_at DESC
		LIMIT $7 OFFSET $8`,
		orgID, clusterID, vmName, namespace, term, engine, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list VM rec history: %w", err)
	}
	defer rows.Close()

	var result []VMRecommendationHistoryRow
	for rows.Next() {
		var row VMRecommendationHistoryRow
		if scanErr := rows.Scan(
			&row.ID, &row.OrgID, &row.ClusterID, &row.VMName, &row.Namespace, &row.Term, &row.Engine,
			&row.RecommendedVCPU, &row.RecommendedMemoryGiB, &row.RecommendedInstanceType,
			&row.GPUClassification, &row.RecommendedGPUAction,
			&row.Category, &row.Confidence, &row.CreatedAt,
		); scanErr != nil {
			return nil, 0, scanErr
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return result, total, nil
}
