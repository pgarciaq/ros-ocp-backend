package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

const (
	// PVC recommendation types.
	PVCRecTypeOversized = "oversized"
	PVCRecTypeNearFull  = "near_full"
	PVCRecTypeOrphaned  = "orphaned"
	PVCRecTypeHealthy   = "healthy"
)

// PVCDigestRow represents a row from daily_pvc_digests.
type PVCDigestRow struct {
	BucketDate    time.Time
	Namespace     string
	PVC           string
	LastSeenPod   string
	VMName        string
	PV            string
	StorageClass  string
	CapacityBytes int64
	RequestBytes  int64
	UsageBytesMin int64
	UsageBytesMax int64
	UsageBytesAvg int64
	SampleCount   int
}

// PVCRec is the output of the PVC recommendation engine.
type PVCRec struct {
	OrgID                        string
	ClusterUUID                  string
	Namespace                    string
	PVC                          string
	LastSeenPod                  string
	VMName                       string
	PV                           string
	StorageClass                 string
	CapacityBytes                int64
	RequestBytes                 int64
	UsageBytesMax                int64
	UsageRatio                   float64
	RecommendationType           string
	RecommendedBytes             *int64
	DaysToFull                   *int
	GrowthBytesPerDay            int64
	EstimatedMonthlySavingsCents int64
	NotificationCodes            []int16
	DataDays                     int
	ConfidenceLevel              float32
	Term                         string
	IdleSince                    *time.Time
	IdleDurationDays             int
	Expl                         PVCExplanationFactors
}

// PVCConfidenceLevel returns 0.0–1.0 based on data coverage vs the term minimum.
func PVCConfidenceLevel(dataDays, minDataDays int) float32 {
	if dataDays <= 0 || minDataDays <= 0 {
		return 0
	}
	ratio := float32(dataDays) / float32(minDataDays)
	if ratio > 1.0 {
		return 1.0
	}
	return ratio
}

func pvcMinClassifyDays(tc TermConfig, settings PVCThresholdSettings) int {
	min := settings.MinTrendDays
	if min < 1 {
		min = 1
	}
	return min
}

// EvaluatePVCNotifications appends low-confidence and other contextual codes to PVC recommendations.
func EvaluatePVCNotifications(rec PVCRec, th NotificationThresholds) []int16 {
	codes := append([]int16(nil), rec.NotificationCodes...)
	if rec.ConfidenceLevel < th.LowConfidenceThreshold && rec.DataDays > 0 {
		codes = append(codes, NotifLowConfidence)
	}
	if rec.DataDays > 0 && rec.DataDays <= th.SparseDataThreshold {
		codes = append(codes, NotifSparseData)
	}
	return codes
}

// RecommendPVCs reads PVC digest data and produces per-term recommendations.
func RecommendPVCs(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, terms []TermConfig) ([]PVCRec, error) {
	pvcSettings, err := ResolvePVCThresholdSettings(ctx, pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("load pvc thresholds: %w", err)
	}
	return recommendPVCsWithSettings(ctx, pool, orgID, clusterUUID, terms, pvcSettings)
}

func recommendPVCsWithSettings(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, terms []TermConfig, settings PVCThresholdSettings) ([]PVCRec, error) {
	rows, err := queryPVCDigests(ctx, pool, orgID, clusterUUID, terms)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	type pvcKey struct {
		Namespace string
		PVC       string
	}
	groups := make(map[pvcKey][]PVCDigestRow)
	for _, r := range rows {
		key := pvcKey{Namespace: r.Namespace, PVC: r.PVC}
		groups[key] = append(groups[key], r)
	}

	var results []PVCRec
	for _, digests := range groups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, tc := range terms {
			windowed := windowDigests(digests, tc.WindowDays)
			rec := computePVCRecommendation(windowed, orgID, clusterUUID, tc, settings)
			results = append(results, rec)
		}
	}
	return results, nil
}

// windowDigests returns the subset of digests within windowDays of the latest bucket_date.
func windowDigests(digests []PVCDigestRow, windowDays int) []PVCDigestRow {
	if len(digests) == 0 {
		return nil
	}
	latest := digests[len(digests)-1].BucketDate
	cutoff := latest.AddDate(0, 0, -windowDays)
	var result []PVCDigestRow
	for _, d := range digests {
		if !d.BucketDate.Before(cutoff) {
			result = append(result, d)
		}
	}
	return result
}

func queryPVCDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, terms []TermConfig) ([]PVCDigestRow, error) {
	lookbackDays := MaxWindowDays(terms, 90)
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

func computePVCRecommendation(digests []PVCDigestRow, orgID, clusterUUID string, tc TermConfig, settings PVCThresholdSettings) PVCRec {
	if len(digests) == 0 {
		return PVCRec{Term: tc.Name, OrgID: orgID, ClusterUUID: clusterUUID, RecommendationType: PVCRecTypeHealthy}
	}

	latest := digests[len(digests)-1]
	rec := PVCRec{
		OrgID:         orgID,
		ClusterUUID:   clusterUUID,
		Namespace:     latest.Namespace,
		PVC:           latest.PVC,
		LastSeenPod:   latest.LastSeenPod,
		VMName:        latest.VMName,
		PV:            latest.PV,
		StorageClass:  latest.StorageClass,
		CapacityBytes: latest.CapacityBytes,
		RequestBytes:  latest.RequestBytes,
		DataDays:      len(digests),
		Term:          tc.Name,
		Expl: PVCExplanationFactors{
			OversizedThresholdBP:      int32(settings.OversizedThreshold * float64(BasisPointsScale)),
			NearFullThresholdBP:       int32(settings.NearFullThreshold * float64(BasisPointsScale)),
			RecommendedSizeMultiplier: int32(settings.RecommendedSizeMultiplier * 100),
			MinRecommendedGiB:         int32(settings.MinRecommendedGiB),
		},
	}

	var maxUsage int64
	allZero := true
	for _, d := range digests {
		if d.UsageBytesMax > maxUsage {
			maxUsage = d.UsageBytesMax
		}
		if d.UsageBytesMax > 0 || d.UsageBytesAvg > 0 {
			allZero = false
		}
	}
	rec.UsageBytesMax = maxUsage

	if latest.CapacityBytes > 0 {
		rec.UsageRatio = float64(maxUsage) / float64(latest.CapacityBytes)
	}

	// Growth trend: require at least MinDataDays with a floor from MinTrendDays (default 2).
	minTrend := tc.MinDataDays
	if minTrend < settings.MinTrendDays {
		minTrend = settings.MinTrendDays
	}
	if len(digests) >= minTrend {
		slope := computePVCGrowthSlope(digests, tc.DecayHalfLifeHours)
		rec.GrowthBytesPerDay = int64(slope)

		if slope > 0 && latest.CapacityBytes > 0 {
			remaining := float64(latest.CapacityBytes) - float64(maxUsage)
			if remaining > 0 {
				daysToFull := int(remaining / slope)
				rec.DaysToFull = &daysToFull
			}
		}
	}

	minClassify := pvcMinClassifyDays(tc, settings)
	switch {
	case allZero && len(digests) >= minClassify:
		rec.RecommendationType = PVCRecTypeOrphaned
		rec.Expl.ClassificationReason = PVCRecTypeOrphaned
		rec.NotificationCodes = append(rec.NotificationCodes, NotifPVCOrphaned)
		rec.IdleSince = findPVCOrphanedSince(digests)
		rec.IdleDurationDays = computeIdleDuration(rec.IdleSince)

	case rec.UsageRatio < settings.OversizedThreshold && len(digests) >= minClassify:
		rec.RecommendationType = PVCRecTypeOversized
		rec.Expl.ClassificationReason = PVCRecTypeOversized
		recommended := maxUsage * int64(settings.RecommendedSizeMultiplier)
		minRecommended := int64(settings.MinRecommendedGiB) << 30
		if recommended < minRecommended {
			recommended = minRecommended
		}
		if recommended < latest.CapacityBytes {
			rec.RecommendedBytes = &recommended
		}
		rec.NotificationCodes = append(rec.NotificationCodes, NotifPVCOversized)

	case rec.UsageRatio > settings.NearFullThreshold:
		rec.RecommendationType = PVCRecTypeNearFull
		rec.Expl.ClassificationReason = PVCRecTypeNearFull
		recommended := maxUsage * int64(settings.RecommendedSizeMultiplier)
		rec.RecommendedBytes = &recommended
		rec.NotificationCodes = append(rec.NotificationCodes, NotifPVCNearFull)

	default:
		rec.RecommendationType = PVCRecTypeHealthy
		rec.Expl.ClassificationReason = PVCRecTypeHealthy
	}

	rec.Expl.DataDays = rec.DataDays

	if rec.DaysToFull != nil && *rec.DaysToFull < settings.DaysToFullAlert && *rec.DaysToFull > 0 {
		rec.NotificationCodes = append(rec.NotificationCodes, NotifPVCNearFull)
	}

	rec.ConfidenceLevel = PVCConfidenceLevel(rec.DataDays, tc.MinDataDays)
	rec.NotificationCodes = EvaluatePVCNotifications(rec, NotificationThresholdsFromSizing(defaultContainerSizingThresholds))

	return rec
}

func pvcIdleDurationArg(days int) any {
	if days <= 0 {
		return nil
	}
	return days
}

// findPVCOrphanedSince returns the first digest date with zero usage in the window.
func findPVCOrphanedSince(digests []PVCDigestRow) *time.Time {
	if len(digests) == 0 {
		return nil
	}
	start := len(digests) - 1
	for start >= 0 && digests[start].UsageBytesMax == 0 && digests[start].UsageBytesAvg == 0 {
		start--
	}
	firstZero := start + 1
	if firstZero >= len(digests) {
		return nil
	}
	t := digests[firstZero].BucketDate
	return &t
}

// computePVCGrowthSlope computes the regression slope of daily average usage
// over time, in bytes per day. When decayHalfLifeHours > 0, exponential-weighted
// least squares is used (recent data is weighted more heavily). When 0, plain
// ordinary least squares is used.
func computePVCGrowthSlope(digests []PVCDigestRow, decayHalfLifeHours float64) float64 {
	n := len(digests)
	if n < 2 {
		return 0.0
	}

	if decayHalfLifeHours <= 0 {
		return computePVCGrowthSlopeOLS(digests)
	}
	return computePVCGrowthSlopeWLS(digests, decayHalfLifeHours)
}

func computePVCGrowthSlopeOLS(digests []PVCDigestRow) float64 {
	n := len(digests)
	var sumX, sumY, sumXY, sumX2 float64
	for i, d := range digests {
		x := float64(i)
		y := float64(d.UsageBytesAvg)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	nf := float64(n)
	denom := nf*sumX2 - sumX*sumX
	if denom == 0 {
		return 0.0
	}
	return (nf*sumXY - sumX*sumY) / denom
}

// computePVCGrowthSlopeWLS uses exponential-weighted least squares where the
// most recent data point (last in digests) has weight 1.0 and older points
// decay according to exp(-ln(2) * age_hours / halflife_hours). Age is measured
// in days (index distance from the last point), converted to hours.
func computePVCGrowthSlopeWLS(digests []PVCDigestRow, halfLifeHours float64) float64 {
	n := len(digests)

	var sumW, sumWX, sumWY, sumWXY, sumWX2 float64
	for i, d := range digests {
		x := float64(i)
		y := float64(d.UsageBytesAvg)
		ageHours := float64(n-1-i) * 24.0
		w := DecayWeight(ageHours, halfLifeHours)
		sumW += w
		sumWX += w * x
		sumWY += w * y
		sumWXY += w * x * y
		sumWX2 += w * x * x
	}

	denom := sumW*sumWX2 - sumWX*sumWX
	if denom == 0 {
		return 0.0
	}
	return (sumW*sumWXY - sumWX*sumWY) / denom
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
		nullIntExpl(rec.Expl.DataDays),
		nullInt32Expl(rec.Expl.OversizedThresholdBP),
		nullInt32Expl(rec.Expl.NearFullThresholdBP),
		nullInt32Expl(rec.Expl.RecommendedSizeMultiplier),
		nullInt32Expl(rec.Expl.MinRecommendedGiB),
		nullStringExpl(rec.Expl.ClassificationReason),
	)
}

func flushPVCRecommendationBatch(ctx context.Context, sender pgxBatchSender, batch *pgx.Batch, chunk []PVCRec) []error {
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
		chunkEnd := chunkStart + db.MaxPgxBatchQueue
		if chunkEnd > len(recs) {
			chunkEnd = len(recs)
		}
		chunk := recs[chunkStart:chunkEnd]
		batch := &pgx.Batch{}
		for _, rec := range chunk {
			queuePVCRecommendationUpsert(batch, rec)
		}
		errs = append(errs, flushPVCRecommendationBatch(ctx, pool, batch, chunk)...)
	}

	// Clean up stale terms for the org+cluster combinations we just wrote.
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
