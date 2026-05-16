package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
)

const (
	// PVCs using less than 20% of capacity are oversized.
	pvcOversizedThreshold = 0.20
	// PVCs using more than 85% of capacity are near-full.
	pvcNearFullThreshold = 0.85
	// Minimum days of data required for a PVC recommendation.
	pvcMinDataDays = 3
	// Minimum days of data for growth trend projection.
	pvcMinTrendDays = 7

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
	OrgID             string
	ClusterUUID       string
	Namespace         string
	PVC               string
	PV                string
	StorageClass      string
	CapacityBytes     int64
	UsageBytesMax     int64
	UsageRatio        float64
	RecommendationType string
	RecommendedBytes  *int64
	DaysToFull        *int
	GrowthBytesPerDay int64
	NotificationCodes []int16
	DataDays          int
}

// RecommendPVCs reads PVC digest data and produces recommendations.
func RecommendPVCs(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]PVCRec, error) {
	rows, err := queryPVCDigests(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	// Group by PVC identity
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
		rec := computePVCRecommendation(digests, orgID, clusterUUID)
		results = append(results, rec)
	}
	return results, nil
}

func queryPVCDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]PVCDigestRow, error) {
	query := `
		SELECT bucket_date, namespace, persistentvolumeclaim, persistentvolume,
			storageclass, capacity_bytes, request_bytes,
			usage_bytes_min, usage_bytes_max, usage_bytes_avg, sample_count
		FROM daily_pvc_digests
		WHERE org_id = $1 AND cluster_uuid = $2
			AND bucket_date >= (CURRENT_DATE - INTERVAL '90 days')
		ORDER BY namespace, persistentvolumeclaim, bucket_date`

	pgRows, err := pool.Query(ctx, query, orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("querying PVC digests: %w", err)
	}
	defer pgRows.Close()

	var results []PVCDigestRow
	for pgRows.Next() {
		var r PVCDigestRow
		if err := pgRows.Scan(
			&r.BucketDate, &r.Namespace, &r.PVC, &r.PV,
			&r.StorageClass, &r.CapacityBytes, &r.RequestBytes,
			&r.UsageBytesMin, &r.UsageBytesMax, &r.UsageBytesAvg, &r.SampleCount,
		); err != nil {
			return nil, fmt.Errorf("scanning PVC digest row: %w", err)
		}
		results = append(results, r)
	}
	return results, pgRows.Err()
}

func computePVCRecommendation(digests []PVCDigestRow, orgID, clusterUUID string) PVCRec {
	if len(digests) == 0 {
		return PVCRec{}
	}

	latest := digests[len(digests)-1]
	rec := PVCRec{
		OrgID:       orgID,
		ClusterUUID: clusterUUID,
		Namespace:   latest.Namespace,
		PVC:         latest.PVC,
		PV:          latest.PV,
		StorageClass: latest.StorageClass,
		CapacityBytes: latest.CapacityBytes,
		DataDays:    len(digests),
	}

	// Find max usage across all days
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

	// Compute usage ratio
	if latest.CapacityBytes > 0 {
		rec.UsageRatio = float64(maxUsage) / float64(latest.CapacityBytes)
	}

	// Compute growth trend if enough data
	if len(digests) >= pvcMinTrendDays {
		slope := computePVCGrowthSlope(digests)
		rec.GrowthBytesPerDay = int64(slope)

		// Project days to full
		if slope > 0 && latest.CapacityBytes > 0 {
			remaining := float64(latest.CapacityBytes) - float64(maxUsage)
			if remaining > 0 {
				daysToFull := int(remaining / slope)
				rec.DaysToFull = &daysToFull
			}
		}
	}

	// Classify recommendation
	switch {
	case allZero && len(digests) >= pvcMinDataDays:
		rec.RecommendationType = PVCRecTypeOrphaned
		rec.NotificationCodes = append(rec.NotificationCodes, NotifPVCOrphaned)

	case rec.UsageRatio < pvcOversizedThreshold && len(digests) >= pvcMinDataDays:
		rec.RecommendationType = PVCRecTypeOversized
		// Recommend 2x the max observed usage (with minimum 1 GiB)
		recommended := maxUsage * 2
		if recommended < 1<<30 {
			recommended = 1 << 30
		}
		if recommended < latest.CapacityBytes {
			rec.RecommendedBytes = &recommended
		}
		rec.NotificationCodes = append(rec.NotificationCodes, NotifPVCOversized)

	case rec.UsageRatio > pvcNearFullThreshold:
		rec.RecommendationType = PVCRecTypeNearFull
		// Recommend expanding to 2x current usage
		recommended := maxUsage * 2
		rec.RecommendedBytes = &recommended
		rec.NotificationCodes = append(rec.NotificationCodes, NotifPVCNearFull)

	default:
		rec.RecommendationType = PVCRecTypeHealthy
	}

	// Add growth warning if days-to-full < 30
	if rec.DaysToFull != nil && *rec.DaysToFull < 30 && *rec.DaysToFull > 0 {
		rec.NotificationCodes = append(rec.NotificationCodes, NotifPVCNearFull)
	}

	return rec
}

// computePVCGrowthSlope computes the linear regression slope of daily average
// usage over time, in bytes per day.
func computePVCGrowthSlope(digests []PVCDigestRow) float64 {
	n := len(digests)
	if n < 2 {
		return 0.0
	}

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

// WritePVCRecommendations upserts PVC recommendations to the database.
func WritePVCRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []PVCRec) error {
	var errs []error
	for _, rec := range recs {
		_, err := pool.Exec(ctx, `
			INSERT INTO pvc_recommendation_sets (
				org_id, cluster_uuid, namespace, persistentvolumeclaim,
				persistentvolume, storageclass, capacity_bytes,
				usage_bytes_max, usage_ratio, recommendation_type,
				recommended_bytes, days_to_full, growth_bytes_per_day,
				notification_codes, data_days, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW())
			ON CONFLICT (org_id, cluster_uuid, namespace, persistentvolumeclaim)
			DO UPDATE SET
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
				updated_at = NOW()`,
			rec.OrgID, rec.ClusterUUID, rec.Namespace, rec.PVC,
			rec.PV, rec.StorageClass, rec.CapacityBytes,
			rec.UsageBytesMax, rec.UsageRatio, rec.RecommendationType,
			rec.RecommendedBytes, rec.DaysToFull, rec.GrowthBytesPerDay,
			rec.NotificationCodes, rec.DataDays,
		)
		if err != nil {
			log.Warnf("WritePVCRecommendations: upsert failed for %s/%s: %v", rec.Namespace, rec.PVC, err)
			errs = append(errs, fmt.Errorf("%s/%s: %w", rec.Namespace, rec.PVC, err))
		}
	}
	return errors.Join(errs...)
}
