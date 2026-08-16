package pgrec

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

type nodeGPUTimeslicingKey struct {
	nodeName string
	gpuModel string
	term     string
}

type nodeContainerRefJSON struct {
	Namespace      string  `json:"namespace"`
	Workload       string  `json:"workload"`
	Container      string  `json:"container"`
	SMActiveAvg    float32 `json:"sm_active_avg"`
	Classification string  `json:"classification"`
}

func gpuContainerRefsJSON(refs []gpu.GPUContainerRef) ([]byte, error) {
	out := make([]nodeContainerRefJSON, len(refs))
	for i, ref := range refs {
		out[i] = nodeContainerRefJSON{
			Namespace:      ref.Namespace,
			Workload:       ref.Workload,
			Container:      ref.Container,
			SMActiveAvg:    ref.SMActiveAvg,
			Classification: string(ref.Classification),
		}
	}
	if out == nil {
		out = []nodeContainerRefJSON{}
	}
	return json.Marshal(out)
}

func timeslicingLastSeen(rec gpu.TimeslicingRec, nodeLastSeen map[string]time.Time) *time.Time {
	if ts, ok := nodeLastSeen[rec.NodeName]; ok && !ts.IsZero() {
		t := ts
		return &t
	}
	return nil
}

// WriteNodeGPUTimeslicingRecs persists live time-slicing rows, history, stale
// deletes, and recommendation_sets cross-references. Empty recs still clear
// cross-refs and delete leftover keys (product SQL, extracted as-is).
func WriteNodeGPUTimeslicingRecs(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	recs []gpu.TimeslicingRec,
	validTerms []string,
	nodeLastSeen map[string]time.Time,
) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for node GPU time-slicing persist: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `
		UPDATE recommendation_sets
		SET time_slicing_node = '', time_slicing_replicas = 0
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND (time_slicing_node <> '' OR time_slicing_replicas <> 0)`,
		orgID, clusterUUID,
	); err != nil {
		return fmt.Errorf("clear time-slicing cross-reference: %w", err)
	}

	currentKeys := make([]nodeGPUTimeslicingKey, 0, len(recs))
	for i := range recs {
		rec := recs[i]
		currentKeys = append(currentKeys, nodeGPUTimeslicingKey{
			nodeName: rec.NodeName,
			gpuModel: rec.GPUModel,
			term:     rec.Term,
		})
		if err := UpsertNodeGPUTimeslicingRec(ctx, tx, orgID, clusterUUID, rec, timeslicingLastSeen(rec, nodeLastSeen)); err != nil {
			return err
		}
		if err := updateTimeslicingCandidateCrossRefs(ctx, tx, orgID, clusterUUID, rec); err != nil {
			return err
		}
	}

	if err := AppendNodeGPUTimeslicingHistory(ctx, tx, orgID, clusterUUID, recs); err != nil {
		return err
	}

	if err := deleteStaleNodeGPUTimeslicingRecs(ctx, tx, orgID, clusterUUID, currentKeys); err != nil {
		return err
	}

	if len(validTerms) > 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM node_gpu_timeslicing_recommendations
			WHERE org_id = $1 AND cluster_uuid = $2
			  AND term != ALL($3)`,
			orgID, clusterUUID, validTerms,
		); err != nil {
			return fmt.Errorf("cleanup stale GPU time-slicing terms: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit node GPU time-slicing persist: %w", err)
	}
	return nil
}

// UpsertNodeGPUTimeslicingRec writes one live time-slicing row.
func UpsertNodeGPUTimeslicingRec(
	ctx context.Context,
	tx pgx.Tx,
	orgID, clusterUUID string,
	rec gpu.TimeslicingRec,
	lastSeenAt *time.Time,
) error {
	candidates, err := gpuContainerRefsJSON(rec.CandidateContainers)
	if err != nil {
		return fmt.Errorf("marshal time-slicing candidates: %w", err)
	}
	impacted, err := gpuContainerRefsJSON(rec.ImpactedContainers)
	if err != nil {
		return fmt.Errorf("marshal time-slicing impacted: %w", err)
	}
	estimatedSavingsCents := Float32USDCentsPtr(rec.TotalNodeSavings)
	savingsPerGPUCents := Float32USDCentsPtr(rec.SavingsPerGPU)

	_, err = tx.Exec(ctx, `
		INSERT INTO node_gpu_timeslicing_recommendations (
			org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, confidence_level,
			candidate_count, impacted_count,
			candidate_containers, impacted_containers,
			notification_codes,
			estimated_savings_cents, savings_per_gpu_cents,
			last_seen_at, updated_at,`+types.NodeGPUTimeslicingExplSQLColumns+`
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10,
			$11, $12,
			$13,
			$14, $15,
			$16, now(), $17, $18, $19, $20
		)
		ON CONFLICT (org_id, cluster_uuid, node_name, gpu_model, term) DO UPDATE SET
			recommended_replicas = EXCLUDED.recommended_replicas,
			confidence = EXCLUDED.confidence,
			confidence_level = EXCLUDED.confidence_level,
			candidate_count = EXCLUDED.candidate_count,
			impacted_count = EXCLUDED.impacted_count,
			candidate_containers = EXCLUDED.candidate_containers,
			impacted_containers = EXCLUDED.impacted_containers,
			notification_codes = EXCLUDED.notification_codes,
			estimated_savings_cents = EXCLUDED.estimated_savings_cents,
			savings_per_gpu_cents = EXCLUDED.savings_per_gpu_cents,
			last_seen_at = EXCLUDED.last_seen_at,
			updated_at = now(),`+types.NodeGPUTimeslicingExplUpdateSet,
		append([]any{
			orgID, clusterUUID, rec.NodeName, rec.GPUModel, rec.Term,
			rec.RecommendedReplicas, rec.Confidence, rec.Confidence,
			len(rec.CandidateContainers), len(rec.ImpactedContainers),
			candidates, impacted,
			rec.NotificationCodes,
			estimatedSavingsCents, savingsPerGPUCents,
			lastSeenAt,
		}, types.AppendNodeGPUTimeslicingExplArgs(nil, rec.Expl)...)...,
	)
	if err != nil {
		return fmt.Errorf("upsert node GPU time-slicing rec %s/%s [%s]: %w", rec.NodeName, rec.GPUModel, rec.Term, err)
	}
	return nil
}

func updateTimeslicingCandidateCrossRefs(
	ctx context.Context,
	tx pgx.Tx,
	orgID, clusterUUID string,
	rec gpu.TimeslicingRec,
) error {
	if len(rec.CandidateContainers) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, cand := range rec.CandidateContainers {
		batch.Queue(`
			UPDATE recommendation_sets
			SET time_slicing_node = $6, time_slicing_replicas = $7
			WHERE org_id = $1 AND cluster_uuid = $2
			  AND namespace = $3 AND workload = $4 AND container_name = $5 AND term = $8`,
			orgID, clusterUUID, cand.Namespace, cand.Workload, cand.Container,
			rec.NodeName, rec.RecommendedReplicas, rec.Term,
		)
	}
	return flushBatch(ctx, tx, batch)
}

// AppendNodeGPUTimeslicingHistory inserts history snapshots inside the caller's transaction.
func AppendNodeGPUTimeslicingHistory(
	ctx context.Context,
	tx pgx.Tx,
	orgID, clusterUUID string,
	recs []gpu.TimeslicingRec,
) error {
	if len(recs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, rec := range recs {
		estimatedSavingsCents := Float32USDCentsPtr(rec.TotalNodeSavings)
		batch.Queue(`
			INSERT INTO node_gpu_timeslicing_recommendation_history (
				org_id, cluster_uuid, node_name, gpu_model, term,
				recommended_replicas, confidence,
				candidate_count, impacted_count,
				estimated_savings_cents,`+types.NodeGPUTimeslicingExplSQLColumns+`
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			append([]any{
				orgID, clusterUUID, rec.NodeName, rec.GPUModel, rec.Term,
				rec.RecommendedReplicas, rec.Confidence,
				len(rec.CandidateContainers), len(rec.ImpactedContainers),
				estimatedSavingsCents,
			}, types.AppendNodeGPUTimeslicingExplArgs(nil, rec.Expl)...)...,
		)
	}
	if err := flushBatch(ctx, tx, batch); err != nil {
		return fmt.Errorf("append node GPU time-slicing history: %w", err)
	}
	return nil
}

func deleteStaleNodeGPUTimeslicingRecs(
	ctx context.Context,
	tx pgx.Tx,
	orgID, clusterUUID string,
	currentKeys []nodeGPUTimeslicingKey,
) error {
	nodes := make([]string, len(currentKeys))
	models := make([]string, len(currentKeys))
	terms := make([]string, len(currentKeys))
	for i, key := range currentKeys {
		nodes[i] = key.nodeName
		models[i] = key.gpuModel
		terms[i] = key.term
	}

	_, err := tx.Exec(ctx, `
		DELETE FROM node_gpu_timeslicing_recommendations t
		WHERE t.org_id = $1 AND t.cluster_uuid = $2
		  AND NOT EXISTS (
			SELECT 1
			FROM unnest($3::text[], $4::text[], $5::text[]) AS k(node_name, gpu_model, term)
			WHERE k.node_name = t.node_name
			  AND k.gpu_model = t.gpu_model
			  AND k.term = t.term
		  )`,
		orgID, clusterUUID, nodes, models, terms,
	)
	if err != nil {
		return fmt.Errorf("delete stale node GPU time-slicing recs: %w", err)
	}
	return nil
}
