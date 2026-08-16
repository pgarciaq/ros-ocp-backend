package pgrec

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

const gpuMIGRecSetUpsertSQL = `
INSERT INTO gpu_mig_recommendation_sets (
	org_id, cluster_uuid, namespace, workload, workload_type,
	container_name, node_name, gpu_model_name, term,
	recommended_gpu_profile, current_gpu_profile,
	gpu_classification, confidence, fb_usage_max_mib, total_fb_mib,
	gpu_idle_state, gpu_idle_since, gpu_idle_duration_days,
	savings_micro_cents, waste_micro_cents,
	category, idle_state, notification_codes,
	last_reported, updated_at
) VALUES (
	$1, $2::uuid, $3, $4, $5,
	$6, $7, $8, $9,
	$10, $11,
	$12, $13, $14, $15,
	$16, $17, $18,
	$19, $20,
	$21, $22, $23,
	$24, $24
)
ON CONFLICT (org_id, cluster_uuid, namespace, workload, container_name, term)
DO UPDATE SET
	workload_type           = EXCLUDED.workload_type,
	node_name               = EXCLUDED.node_name,
	gpu_model_name          = EXCLUDED.gpu_model_name,
	recommended_gpu_profile = EXCLUDED.recommended_gpu_profile,
	current_gpu_profile     = EXCLUDED.current_gpu_profile,
	gpu_classification      = EXCLUDED.gpu_classification,
	confidence              = EXCLUDED.confidence,
	fb_usage_max_mib        = EXCLUDED.fb_usage_max_mib,
	total_fb_mib            = EXCLUDED.total_fb_mib,
	gpu_idle_state          = EXCLUDED.gpu_idle_state,
	gpu_idle_since          = EXCLUDED.gpu_idle_since,
	gpu_idle_duration_days  = EXCLUDED.gpu_idle_duration_days,
	savings_micro_cents     = EXCLUDED.savings_micro_cents,
	waste_micro_cents       = EXCLUDED.waste_micro_cents,
	category                = EXCLUDED.category,
	idle_state              = EXCLUDED.idle_state,
	notification_codes      = EXCLUDED.notification_codes,
	last_reported           = EXCLUDED.last_reported,
	updated_at              = EXCLUDED.updated_at
`

// GPUMIGRow is one gpu_mig_recommendation_sets upsert. Processor maps after
// re-query + costdata; CLI maps from in-memory GPURecs via WriteGPURecs.
type GPUMIGRow struct {
	OrgID               string
	ClusterUUID         string
	Namespace           string
	Workload            string
	WorkloadType        string
	ContainerName       string
	NodeName            string
	GPUModelName        string
	Term                string
	RecommendedProfile  string
	CurrentProfile      string
	Classification      string
	Confidence          float32
	FBUsageMaxMiB       float32
	TotalFBMiB          *int64
	GPUIdleState        string
	GPUIdleSince        *time.Time
	GPUIdleDurationDays int
	SavingsMicroCents   int64
	WasteMicroCents     int64
	Category            string
	IdleState           string
	NotificationCodes   []int16
	LastReported        time.Time
}

// GPURecWrite pairs container identity with an in-memory GPURec for CLI persist.
type GPURecWrite struct {
	Namespace     string
	Workload      string
	WorkloadType  string
	ContainerName string
	NodeName      string
	Rec           gpu.GPURec
}

// WriteGPURecs maps in-memory GPU recs (no Postgres re-query, no costdata) and
// upserts rows that have a MIG profile recommendation.
func WriteGPURecs(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, rows []GPURecWrite, lastReported time.Time) error {
	if lastReported.IsZero() {
		lastReported = time.Now().UTC()
	}
	writes := make([]GPUMIGRow, 0, len(rows))
	for _, row := range rows {
		rec := row.Rec
		if !rec.HasMIGRecommendation() {
			continue
		}
		gpuIdle := string(rec.GPUIdleState)
		if gpuIdle == "" {
			gpuIdle = string(types.IdleStateActive)
		}
		classification := string(rec.Classification)
		if classification == "" {
			classification = string(gpu.GPUClassNoProfiling)
		}
		var totalFB *int64
		if spec := gpu.MatchGPUModel(rec.GPUModelName); spec != nil {
			v := int64(spec.TotalFBMiB)
			totalFB = &v
		}
		var savings int64
		if rec.EstimatedGPUSavingsCents != nil {
			savings = *rec.EstimatedGPUSavingsCents
		}
		writes = append(writes, GPUMIGRow{
			OrgID:               orgID,
			ClusterUUID:         clusterUUID,
			Namespace:           row.Namespace,
			Workload:            row.Workload,
			WorkloadType:        row.WorkloadType,
			ContainerName:       row.ContainerName,
			NodeName:            row.NodeName,
			GPUModelName:        rec.GPUModelName,
			Term:                rec.Term,
			RecommendedProfile:  rec.RecommendedGPUProfile,
			CurrentProfile:      rec.CurrentGPUProfile,
			Classification:      classification,
			Confidence:          rec.Confidence,
			FBUsageMaxMiB:       rec.FBUsageMaxMiB,
			TotalFBMiB:          totalFB,
			GPUIdleState:        gpuIdle,
			GPUIdleSince:        rec.GPUIdleSince,
			GPUIdleDurationDays: rec.GPUIdleDurationDays,
			SavingsMicroCents:   savings,
			WasteMicroCents:     rec.GPUEstimatedWasteCents,
			NotificationCodes:   rec.NotificationCodes,
			LastReported:        lastReported,
		})
	}
	return WriteGPUMIGRecommendationSets(ctx, pool, writes)
}

// WriteGPUMIGRecommendationSets batch-upserts already-mapped MIG rec rows.
func WriteGPUMIGRecommendationSets(ctx context.Context, pool *pgxpool.Pool, writes []GPUMIGRow) error {
	if len(writes) == 0 {
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for GPU MIG recs: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for chunkStart := 0; chunkStart < len(writes); chunkStart += maxPgxBatchQueue {
		chunkEnd := min(chunkStart+maxPgxBatchQueue, len(writes))
		chunk := writes[chunkStart:chunkEnd]
		batch := &pgx.Batch{}
		for _, w := range chunk {
			batch.Queue(gpuMIGRecSetUpsertSQL,
				w.OrgID, w.ClusterUUID, w.Namespace, w.Workload, w.WorkloadType,
				w.ContainerName, w.NodeName, w.GPUModelName, w.Term,
				w.RecommendedProfile, w.CurrentProfile,
				w.Classification, w.Confidence, w.FBUsageMaxMiB, w.TotalFBMiB,
				w.GPUIdleState, w.GPUIdleSince, w.GPUIdleDurationDays,
				w.SavingsMicroCents, w.WasteMicroCents,
				w.Category, w.IdleState, w.NotificationCodes,
				w.LastReported,
			)
		}
		if err := flushBatch(ctx, tx, batch); err != nil {
			return fmt.Errorf("GPU MIG rec batch chunk %d: %w", chunkStart, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit GPU MIG recs: %w", err)
	}
	return nil
}
