package pvc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	libpvc "github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
)

// RecommendPVCs reads PVC digest data and produces per-term recommendations.
// notifThresholds controls notification code evaluation.
func RecommendPVCs(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, terms []core.TermConfig, settings ThresholdSettings, notifThresholds core.NotificationThresholds) ([]PVCRec, error) {
	rows, err := queryPVCDigests(ctx, pool, orgID, clusterUUID, terms)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	groups := make(map[PVCKey][]PVCDigestRow)
	for _, r := range rows {
		key := PVCKey{Namespace: r.Namespace, PVC: r.PVC}
		groups[key] = append(groups[key], r)
	}

	return libpvc.RecommendPVCs(ctx, groups, EngineConfig{
		OrgID:           orgID,
		ClusterUUID:     clusterUUID,
		Terms:           terms,
		Settings:        settings,
		NotifThresholds: notifThresholds,
	})
}

func queryPVCDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, terms []core.TermConfig) ([]PVCDigestRow, error) {
	lookbackDays := core.MaxWindowDays(terms, 90)
	query := fmt.Sprintf(`
		SELECT bucket_date, namespace, persistentvolumeclaim, last_seen_pod, vm_name, persistentvolume,
			storageclass, capacity_bytes, request_bytes,
			usage_bytes_min, usage_bytes_max, usage_bytes_avg, sample_count
		FROM daily_pvc_digests
		WHERE org_id = $1 AND cluster_uuid = $2
			AND bucket_date >= (CURRENT_DATE - INTERVAL '%d days')
		ORDER BY namespace, persistentvolumeclaim, bucket_date`, lookbackDays)

	pgRows, err := pool.Query(ctx, query, orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("querying PVC digests: %w", err)
	}
	defer pgRows.Close()

	var results []PVCDigestRow
	for pgRows.Next() {
		var r PVCDigestRow
		if err := pgRows.Scan(
			&r.BucketDate, &r.Namespace, &r.PVC, &r.LastSeenPod, &r.VMName, &r.PV,
			&r.StorageClass, &r.CapacityBytes, &r.RequestBytes,
			&r.UsageBytesMin, &r.UsageBytesMax, &r.UsageBytesAvg, &r.SampleCount,
		); err != nil {
			return nil, fmt.Errorf("scanning PVC digest row: %w", err)
		}
		results = append(results, r)
	}
	return results, pgRows.Err()
}

func pvcIdleDurationArg(days int) any {
	if days <= 0 {
		return nil
	}
	return days
}

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

func queuePVCRecommendationUpsert(batch *pgx.Batch, rec PVCRec) {
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
		core.NullIntExpl(rec.Expl.DataDays),
		core.NullInt32Expl(rec.Expl.OversizedThresholdBP),
		core.NullInt32Expl(rec.Expl.NearFullThresholdBP),
		core.NullInt32Expl(rec.Expl.RecommendedSizeMultiplier),
		core.NullInt32Expl(rec.Expl.MinRecommendedGiB),
		core.NullStringExpl(rec.Expl.ClassificationReason),
	)
}

func flushPVCRecommendationBatch(ctx context.Context, sender db.PgxBatchSender, batch *pgx.Batch, chunk []PVCRec) []error {
	if len(chunk) == 0 {
		return nil
	}
	br := sender.SendBatch(ctx, batch)
	defer br.Close()

	var errs []error
	for i := range chunk {
		rec := chunk[i]
		if _, err := br.Exec(); err != nil {
			logging.ForOrg(rec.OrgID, rec.ClusterUUID).Warnf(
				"WritePVCRecommendations: upsert failed for %s/%s [%s]: %v",
				rec.Namespace, rec.PVC, rec.Term, err,
			)
			errs = append(errs, fmt.Errorf("%s/%s [%s]: %w", rec.Namespace, rec.PVC, rec.Term, err))
		}
	}
	return errs
}

// WritePVCRecommendations upserts PVC recommendations to the database and
// removes rows for terms no longer in the active configuration.
func WritePVCRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []PVCRec, validTerms []string) error {
	if len(recs) == 0 {
		return nil
	}

	t0 := time.Now()
	defer func() { metrics.ObserveDB("write_pvc_recommendations", t0) }()

	var errs []error
	for chunkStart := 0; chunkStart < len(recs); chunkStart += db.MaxPgxBatchQueue {
		chunkEnd := min(chunkStart+db.MaxPgxBatchQueue, len(recs))
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
