package pgrec

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

const pvcRecommendationUpsertSQL = `
	INSERT INTO pvc_recommendation_sets (
		org_id, cluster_uuid, namespace, persistentvolumeclaim,
		last_seen_pod, vm_name, persistentvolume, storageclass, capacity_bytes,
		usage_bytes_max, usage_ratio, recommendation_type,
		recommended_bytes, days_to_full, growth_bytes_per_day,
		notification_codes, data_days, term,
		estimated_savings_cents,
		idle_since, idle_duration_days,
		expl_data_days, expl_oversized_threshold_bp, expl_near_full_threshold_bp,
		expl_recommended_size_multiplier, expl_min_recommended_gib, expl_classification_reason,
		updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, NOW())
	ON CONFLICT (org_id, cluster_uuid, namespace, persistentvolumeclaim, term)
	DO UPDATE SET
		last_seen_pod = CASE
			WHEN EXCLUDED.last_seen_pod != '' THEN EXCLUDED.last_seen_pod
			ELSE pvc_recommendation_sets.last_seen_pod
		END,
		vm_name = CASE
			WHEN EXCLUDED.vm_name != '' THEN EXCLUDED.vm_name
			ELSE pvc_recommendation_sets.vm_name
		END,
		persistentvolume = EXCLUDED.persistentvolume,
		storageclass = EXCLUDED.storageclass,
		capacity_bytes = EXCLUDED.capacity_bytes,
		usage_bytes_max = EXCLUDED.usage_bytes_max,
		usage_ratio = EXCLUDED.usage_ratio,
		recommendation_type = EXCLUDED.recommendation_type,
		recommended_bytes = EXCLUDED.recommended_bytes,
		days_to_full = EXCLUDED.days_to_full,
		growth_bytes_per_day = EXCLUDED.growth_bytes_per_day,
		notification_codes = EXCLUDED.notification_codes,
		data_days = EXCLUDED.data_days,
		estimated_savings_cents = EXCLUDED.estimated_savings_cents,
		idle_since = EXCLUDED.idle_since,
		idle_duration_days = EXCLUDED.idle_duration_days,
		expl_data_days = EXCLUDED.expl_data_days,
		expl_oversized_threshold_bp = EXCLUDED.expl_oversized_threshold_bp,
		expl_near_full_threshold_bp = EXCLUDED.expl_near_full_threshold_bp,
		expl_recommended_size_multiplier = EXCLUDED.expl_recommended_size_multiplier,
		expl_min_recommended_gib = EXCLUDED.expl_min_recommended_gib,
		expl_classification_reason = EXCLUDED.expl_classification_reason,
		updated_at = NOW()`

func queuePVCRecommendationUpsert(batch *pgx.Batch, rec pvc.PVCRec) {
	notificationCodes := rec.NotificationCodes
	if notificationCodes == nil {
		notificationCodes = []int16{}
	}
	batch.Queue(pvcRecommendationUpsertSQL,
		rec.OrgID, rec.ClusterUUID, rec.Namespace, rec.PVC,
		rec.LastSeenPod, rec.VMName, rec.PV, rec.StorageClass, rec.CapacityBytes,
		rec.UsageBytesMax, rec.UsageRatio, rec.RecommendationType,
		rec.RecommendedBytes, rec.DaysToFull, rec.GrowthBytesPerDay,
		notificationCodes, rec.DataDays, rec.Term,
		rec.EstimatedMonthlySavingsCents,
		rec.IdleSince, pvcIdleDurationArg(rec.IdleDurationDays),
		types.NullIntExpl(rec.Expl.DataDays),
		types.NullInt32Expl(rec.Expl.OversizedThresholdBP),
		types.NullInt32Expl(rec.Expl.NearFullThresholdBP),
		types.NullInt32Expl(rec.Expl.RecommendedSizeMultiplier),
		types.NullInt32Expl(rec.Expl.MinRecommendedGiB),
		types.NullStringExpl(rec.Expl.ClassificationReason),
	)
}

func flushPVCRecommendationBatch(ctx context.Context, sender pgxBatchSender, batch *pgx.Batch, chunk []pvc.PVCRec) []error {
	if len(chunk) == 0 {
		return nil
	}
	br := sender.SendBatch(ctx, batch)
	defer br.Close()

	var errs []error
	for i := range chunk {
		rec := chunk[i]
		if _, err := br.Exec(); err != nil {
			errs = append(errs, fmt.Errorf("%s/%s [%s]: %w", rec.Namespace, rec.PVC, rec.Term, err))
		}
	}
	return errs
}

// WritePVCRecommendations upserts PVC recommendations and removes rows for
// terms no longer in validTerms.
func WritePVCRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []pvc.PVCRec, validTerms []string) error {
	if len(recs) == 0 {
		return nil
	}

	var errs []error
	for chunkStart := 0; chunkStart < len(recs); chunkStart += maxPgxBatchQueue {
		chunkEnd := min(chunkStart+maxPgxBatchQueue, len(recs))
		chunk := recs[chunkStart:chunkEnd]
		batch := &pgx.Batch{}
		for _, rec := range chunk {
			queuePVCRecommendationUpsert(batch, rec)
		}
		errs = append(errs, flushPVCRecommendationBatch(ctx, pool, batch, chunk)...)
	}

	if len(validTerms) > 0 {
		orgID := recs[0].OrgID
		clusterUUID := recs[0].ClusterUUID
		_, err := pool.Exec(ctx,
			`DELETE FROM pvc_recommendation_sets
			 WHERE org_id = $1 AND cluster_uuid = $2
			   AND term != ALL($3)`,
			orgID, clusterUUID, validTerms,
		)
		if err != nil {
			errs = append(errs, fmt.Errorf("cleanup stale PVC terms: %w", err))
		}
	}

	return errors.Join(errs...)
}
