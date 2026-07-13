package gpu

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

type nodeGPUTimeslicingKey struct {
	nodeName string
	gpuModel string
	term     string
}

// ComputeAndPersistNodeGPUTimeSlicingRecs computes node GPU time-slicing recommendations
// for a cluster and persists live rows, history, and recommendation_sets cross-references.
func ComputeAndPersistNodeGPUTimeSlicingRecs(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	terms []core.TermConfig,
	costData *costdata.ClusterCostData,
) error {
	t0 := time.Now()
	defer func() { metrics.ObserveDB("persist_node_gpu_timeslicing_recs", t0) }()

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -core.MaxWindowDays(terms, 30))

	validTerms := make([]string, len(terms))
	for i, tc := range terms {
		validTerms[i] = tc.Name
	}

	settings, err := ResolveGPUThresholdSettings(ctx, pool, orgID)
	if err != nil {
		settings = defaultGPUThresholdSettings
	}

	var gpuRate *float32
	if costData != nil {
		if rate := GPUMonthlyRate(costData); rate > 0 {
			r := float32(rate)
			gpuRate = &r
		}
	}

	gpuRecs, nodeMap, nodeLastSeen, err := QueryGPURecommendations(ctx, pool, orgID, clusterUUID, start, now, terms, nil)
	if err != nil {
		return fmt.Errorf("query GPU recommendations for time-slicing persist: %w", err)
	}

	groups := GroupGPURecsByNodeAndModel(gpuRecs, nodeMap, nodeLastSeen, clusterUUID)
	recs := make([]*TimeslicingRec, 0, len(groups))
	for _, group := range groups {
		if tsRec := ComputeNodeTimeslicingRecWithSettings(group, gpuRate, now, settings); tsRec != nil {
			recs = append(recs, tsRec)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for node GPU time-slicing persist: %w", err)
	}
	defer tx.Rollback(ctx)

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
	for _, rec := range recs {
		currentKeys = append(currentKeys, nodeGPUTimeslicingKey{
			nodeName: rec.NodeName,
			gpuModel: rec.GPUModel,
			term:     rec.Term,
		})
		if err := upsertNodeGPUTimeslicingRec(ctx, tx, orgID, clusterUUID, rec, groupLastSeen(rec, nodeLastSeen)); err != nil {
			return err
		}
		if err := updateTimeslicingCandidateCrossRefs(ctx, tx, orgID, clusterUUID, rec); err != nil {
			return err
		}
	}

	if err := appendNodeGPUTimeslicingHistory(ctx, tx, orgID, clusterUUID, recs); err != nil {
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

	logging.ForOrg(orgID, clusterUUID).Infof(
		"ComputeAndPersistNodeGPUTimeSlicingRecs: upserted %d recs", len(recs),
	)
	return nil
}

func groupLastSeen(rec *TimeslicingRec, nodeLastSeen map[string]time.Time) *time.Time {
	if rec == nil {
		return nil
	}
	if ts, ok := nodeLastSeen[rec.NodeName]; ok && !ts.IsZero() {
		t := ts
		return &t
	}
	return nil
}

func upsertNodeGPUTimeslicingRec(
	ctx context.Context,
	tx pgx.Tx,
	orgID, clusterUUID string,
	rec *TimeslicingRec,
	lastSeenAt *time.Time,
) error {
	candidates := gpuContainerRefsToModel(rec.CandidateContainers)
	impacted := gpuContainerRefsToModel(rec.ImpactedContainers)
	estimatedSavingsCents := float32USDCentsPtr(rec.TotalNodeSavings)
	savingsPerGPUCents := float32USDCentsPtr(rec.SavingsPerGPU)

	_, err := tx.Exec(ctx, `
		INSERT INTO node_gpu_timeslicing_recommendations (
			org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, confidence_level,
			candidate_count, impacted_count,
			candidate_containers, impacted_containers,
			notification_codes,
			estimated_savings_cents, savings_per_gpu_cents,
			last_seen_at, updated_at,`+core.NodeGPUTimeslicingExplSQLColumns+`
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
			updated_at = now(),`+core.NodeGPUTimeslicingExplUpdateSet,
		append([]any{
			orgID, clusterUUID, rec.NodeName, rec.GPUModel, rec.Term,
			rec.RecommendedReplicas, rec.Confidence, rec.Confidence,
			len(rec.CandidateContainers), len(rec.ImpactedContainers),
			model.NodeContainerRefList(candidates), model.NodeContainerRefList(impacted),
			rec.NotificationCodes,
			estimatedSavingsCents, savingsPerGPUCents,
			lastSeenAt,
		}, core.AppendNodeGPUTimeslicingExplArgs(nil, rec.Expl)...)...,
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
	rec *TimeslicingRec,
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
	br := tx.SendBatch(ctx, batch)
	defer br.Close()
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("update time-slicing cross-ref batch row %d: %w", i, err)
		}
	}
	return nil
}

func appendNodeGPUTimeslicingHistory(
	ctx context.Context,
	tx pgx.Tx,
	orgID, clusterUUID string,
	recs []*TimeslicingRec,
) error {
	if len(recs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, rec := range recs {
		estimatedSavingsCents := float32USDCentsPtr(rec.TotalNodeSavings)
		batch.Queue(`
			INSERT INTO node_gpu_timeslicing_recommendation_history (
				org_id, cluster_uuid, node_name, gpu_model, term,
				recommended_replicas, confidence,
				candidate_count, impacted_count,
				estimated_savings_cents,`+core.NodeGPUTimeslicingExplSQLColumns+`
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			append([]any{
				orgID, clusterUUID, rec.NodeName, rec.GPUModel, rec.Term,
				rec.RecommendedReplicas, rec.Confidence,
				len(rec.CandidateContainers), len(rec.ImpactedContainers),
				estimatedSavingsCents,
			}, core.AppendNodeGPUTimeslicingExplArgs(nil, rec.Expl)...)...,
		)
	}
	br := tx.SendBatch(ctx, batch)
	defer br.Close()
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("append node GPU time-slicing history batch row %d: %w", i, err)
		}
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

func gpuContainerRefsToModel(refs []GPUContainerRef) []model.NodeContainerRef {
	if len(refs) == 0 {
		return []model.NodeContainerRef{}
	}
	out := make([]model.NodeContainerRef, len(refs))
	for i, ref := range refs {
		out[i] = model.NodeContainerRef{
			Namespace:      ref.Namespace,
			Workload:       ref.Workload,
			Container:      ref.Container,
			SMActiveAvg:    ref.SMActiveAvg,
			Classification: string(ref.Classification),
		}
	}
	return out
}

func float32USDCentsPtr(v *float32) *int64 {
	if v == nil {
		return nil
	}
	cents := money.USDToCents(float64(*v))
	return &cents
}
