package engine

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// pvcQualityKey uniquely identifies a PVC within a cluster.
type pvcQualityKey struct {
	Namespace string
	PVCName   string
}

// OldPVCRecommendation holds previous recommendation values read from
// pvc_recommendation_sets before WritePVCRecommendations overwrites them.
type OldPVCRecommendation struct {
	RecommendedBytes *int64
	CapacityBytes    int64
	UpdatedAt        time.Time
}

// ReadClusterOldPVCRecommendations fetches existing pvc_recommendation_sets
// rows for a cluster (short term only) before they are overwritten. Returns a
// map keyed by (namespace, pvc_name).
func ReadClusterOldPVCRecommendations(
	ctx context.Context, pool *pgxpool.Pool,
	orgID, clusterUUID string,
) (map[pvcQualityKey]OldPVCRecommendation, error) {
	rows, err := pool.Query(ctx, `
		SELECT namespace, persistentvolumeclaim,
			recommended_bytes, capacity_bytes, updated_at
		FROM pvc_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND term = 'medium'`,
		orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("ReadClusterOldPVCRecommendations: %w", err)
	}
	defer rows.Close()

	result := make(map[pvcQualityKey]OldPVCRecommendation, 64)
	for rows.Next() {
		var ns, pvc string
		var old OldPVCRecommendation
		if err := rows.Scan(&ns, &pvc, &old.RecommendedBytes, &old.CapacityBytes, &old.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ReadClusterOldPVCRecommendations scan: %w", err)
		}
		result[pvcQualityKey{Namespace: ns, PVCName: pvc}] = old
	}
	return result, rows.Err()
}

// ComputePVCStability compares old vs new recommended_bytes.
// stability = max(0, 1.0 - |variation|/100)
// Returns 1.0 if either value is nil (no recommendation to compare).
func ComputePVCStability(oldRecommendedBytes, newRecommendedBytes *int64) float32 {
	if oldRecommendedBytes == nil || newRecommendedBytes == nil {
		return 1.0
	}
	if *oldRecommendedBytes == 0 {
		return 1.0
	}
	variationPct := math.Abs(float64(*newRecommendedBytes-*oldRecommendedBytes) / float64(*oldRecommendedBytes) * 100)
	v := 1.0 - variationPct/100
	if v < 0 {
		return 0
	}
	return float32(v)
}

// DetectPVCAdoption returns true if current capacity ≈ old recommended_bytes
// within a 5% tolerance.
func DetectPVCAdoption(currentCapacityBytes int64, oldRecommendedBytes *int64) bool {
	if oldRecommendedBytes == nil {
		return false
	}
	return withinTolerance(currentCapacityBytes, *oldRecommendedBytes, 0.05)
}

// CountPVCDaysAboveThreshold counts days in the digest window where
// usage_ratio (usage_bytes_max / capacity_bytes) exceeds the threshold.
func CountPVCDaysAboveThreshold(digests []PVCDigestRow, threshold float64) int64 {
	var count int64
	for _, d := range digests {
		if d.CapacityBytes <= 0 {
			continue
		}
		ratio := float64(d.UsageBytesMax) / float64(d.CapacityBytes)
		if ratio > threshold {
			count++
		}
	}
	return count
}

// PVCQualityRow represents a row to be inserted into pvc_recommendation_quality.
type PVCQualityRow struct {
	MeasuredAt           time.Time
	OrgID                string
	ClusterUUID          string
	Namespace            string
	PVCName              string
	Engine               string
	StabilityPct         float32
	AdoptionDetected     bool
	DaysAboveThreshold   int64
	RecommendationAgeHrs int64
}

// WritePVCQuality batch-inserts quality metrics into pvc_recommendation_quality.
func WritePVCQuality(ctx context.Context, pool *pgxpool.Pool, qualityRows []PVCQualityRow) error {
	if len(qualityRows) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, r := range qualityRows {
		batch.Queue(`
			INSERT INTO pvc_recommendation_quality (
				measured_at, org_id, cluster_uuid, namespace, pvc_name, engine,
				stability_pct, adoption_detected, days_above_threshold, recommendation_age_hours
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (org_id, cluster_uuid, namespace, pvc_name, engine, measured_at)
			DO UPDATE SET
				stability_pct = EXCLUDED.stability_pct,
				adoption_detected = EXCLUDED.adoption_detected,
				days_above_threshold = EXCLUDED.days_above_threshold,
				recommendation_age_hours = EXCLUDED.recommendation_age_hours`,
			r.MeasuredAt, r.OrgID, r.ClusterUUID, r.Namespace, r.PVCName, r.Engine,
			r.StabilityPct, r.AdoptionDetected, r.DaysAboveThreshold, r.RecommendationAgeHrs,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			if isPartitionMissing(err) {
				logging.GetLogger().Errorf("WritePVCQuality: missing partition for pvc_recommendation_quality: %v", err)
				return fmt.Errorf("partition missing for pvc_recommendation_quality: %w", err)
			}
			return fmt.Errorf("WritePVCQuality batch exec: %w", err)
		}
	}
	return nil
}

// BuildPVCQualityRows computes quality metrics for a set of PVC recommendations
// by comparing them against old recommendations and digests.
func BuildPVCQualityRows(
	recs []PVCRec,
	oldRecs map[pvcQualityKey]OldPVCRecommendation,
	digestsByPVC map[pvcQualityKey][]PVCDigestRow,
) []PVCQualityRow {
	if len(recs) == 0 {
		return nil
	}

	nowClock := time.Now().UTC()
	measuredAt := time.Date(nowClock.Year(), nowClock.Month(), nowClock.Day(), 0, 0, 0, 0, time.UTC)

	type qk struct {
		key    pvcQualityKey
		engine string
	}
	seen := map[qk]bool{}
	var rows []PVCQualityRow

	for _, r := range recs {
		key := pvcQualityKey{Namespace: r.Namespace, PVCName: r.PVC}
		engine := "cost"
		k := qk{key: key, engine: engine}
		if seen[k] {
			continue
		}
		seen[k] = true

		var stabilityPct float32 = 1.0
		var adopted bool
		var ageHours int64

		if old, ok := oldRecs[key]; ok {
			stabilityPct = ComputePVCStability(old.RecommendedBytes, r.RecommendedBytes)
			adopted = DetectPVCAdoption(r.CapacityBytes, old.RecommendedBytes)
			ageHours = ComputeRecommendationAgeHours(old.UpdatedAt, nowClock)
		}

		var daysAbove int64
		if digests, ok := digestsByPVC[key]; ok {
			daysAbove = CountPVCDaysAboveThreshold(digests, 0.95)
		}

		rows = append(rows, PVCQualityRow{
			MeasuredAt:           measuredAt,
			OrgID:                r.OrgID,
			ClusterUUID:          r.ClusterUUID,
			Namespace:            r.Namespace,
			PVCName:              r.PVC,
			Engine:               engine,
			StabilityPct:         stabilityPct,
			AdoptionDetected:     adopted,
			DaysAboveThreshold:   daysAbove,
			RecommendationAgeHrs: ageHours,
		})
	}
	return rows
}
