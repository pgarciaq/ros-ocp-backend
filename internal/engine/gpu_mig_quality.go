package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// OldGPUMIGRecommendation holds previous recommendation values read from
// recommendation_sets before StoreGPUClassifications overwrites them.
type OldGPUMIGRecommendation struct {
	RecommendedGPUProfile string
	CurrentGPUProfile     string
	UpdatedAt             time.Time
}

// ReadClusterOldGPUMIGRecommendations fetches existing recommendation_sets rows
// for GPU containers (those with a non-empty recommended_gpu_profile) before
// they are overwritten.
func ReadClusterOldGPUMIGRecommendations(
	ctx context.Context, pool *pgxpool.Pool,
	orgID, clusterUUID string,
) (map[GPUContainerKey]OldGPUMIGRecommendation, error) {
	rows, err := pool.Query(ctx, `
		SELECT namespace, workload, container_name,
			COALESCE(recommended_gpu_profile, ''), updated_at
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2
			AND has_gpu = true
			AND recommended_gpu_profile IS NOT NULL
			AND recommended_gpu_profile != ''`,
		orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("ReadClusterOldGPUMIGRecommendations: %w", err)
	}
	defer rows.Close()

	result := make(map[GPUContainerKey]OldGPUMIGRecommendation, 64)
	for rows.Next() {
		var ns, wl, cn string
		var old OldGPUMIGRecommendation
		if err := rows.Scan(&ns, &wl, &cn, &old.RecommendedGPUProfile, &old.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ReadClusterOldGPUMIGRecommendations scan: %w", err)
		}
		result[GPUContainerKey{Namespace: ns, Workload: wl, ContainerName: cn}] = old
	}
	return result, rows.Err()
}

// ReadCurrentGPUProfiles reads the current gpu_mig_profile from
// gpu_container_digests (most recent per container) for adoption detection.
func ReadCurrentGPUProfiles(
	ctx context.Context, pool *pgxpool.Pool,
	orgID, clusterUUID string,
) (map[GPUContainerKey]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT ON (namespace, workload, container_name)
			namespace, workload, container_name,
			COALESCE(gpu_profile_name, '')
		FROM gpu_container_digests
		WHERE cluster_uuid = $1
		ORDER BY namespace, workload, container_name, interval_start DESC`,
		clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("ReadCurrentGPUProfiles: %w", err)
	}
	defer rows.Close()

	result := make(map[GPUContainerKey]string, 64)
	for rows.Next() {
		var ns, wl, cn, profile string
		if err := rows.Scan(&ns, &wl, &cn, &profile); err != nil {
			return nil, fmt.Errorf("ReadCurrentGPUProfiles scan: %w", err)
		}
		result[GPUContainerKey{Namespace: ns, Workload: wl, ContainerName: cn}] = profile
	}
	return result, rows.Err()
}

// ComputeGPUMIGStability compares old vs new recommended_gpu_profile.
// Binary: 1.0 if same, 0.0 if different (MIG profiles are discrete values).
func ComputeGPUMIGStability(oldProfile, newProfile string) float32 {
	if oldProfile == "" || newProfile == "" {
		return 1.0
	}
	if oldProfile == newProfile {
		return 1.0
	}
	return 0.0
}

// DetectGPUMIGAdoption returns true if the current GPU profile matches
// the old recommended GPU profile (exact string match).
func DetectGPUMIGAdoption(currentProfile, oldRecommendedProfile string) bool {
	if oldRecommendedProfile == "" {
		return false
	}
	return currentProfile == oldRecommendedProfile
}

// CountGPUContentionDays counts days in gpu_container_digests where
// sm_active_max >= 9500 basis points (95%) indicating GPU contention.
func CountGPUContentionDays(
	ctx context.Context, pool *pgxpool.Pool,
	clusterUUID, namespace, workload, containerName string,
	since time.Time,
) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT interval_start)
		FROM gpu_container_digests
		WHERE cluster_uuid = $1
			AND namespace = $2
			AND workload = $3
			AND container_name = $4
			AND interval_start >= $5
			AND sm_active_max >= 9500`,
		clusterUUID, namespace, workload, containerName, since.Format("2006-01-02"),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("CountGPUContentionDays: %w", err)
	}
	return count, nil
}

// GPUMIGQualityRow represents a row to be inserted into gpu_mig_recommendation_quality.
type GPUMIGQualityRow struct {
	MeasuredAt           time.Time
	OrgID                string
	ClusterUUID          string
	Namespace            string
	Workload             string
	ContainerName        string
	Engine               string
	StabilityPct         float32
	AdoptionDetected     bool
	ContentionDays       int64
	RecommendationAgeHrs int64
}

// WriteGPUMIGQuality batch-inserts quality metrics into gpu_mig_recommendation_quality.
func WriteGPUMIGQuality(ctx context.Context, pool *pgxpool.Pool, qualityRows []GPUMIGQualityRow) error {
	if len(qualityRows) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, r := range qualityRows {
		batch.Queue(`
			INSERT INTO gpu_mig_recommendation_quality (
				measured_at, org_id, cluster_uuid, namespace, workload, container_name, engine,
				stability_pct, adoption_detected, contention_days, recommendation_age_hours
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, container_name, engine, measured_at)
			DO UPDATE SET
				stability_pct = EXCLUDED.stability_pct,
				adoption_detected = EXCLUDED.adoption_detected,
				contention_days = EXCLUDED.contention_days,
				recommendation_age_hours = EXCLUDED.recommendation_age_hours`,
			r.MeasuredAt, r.OrgID, r.ClusterUUID, r.Namespace, r.Workload, r.ContainerName, r.Engine,
			r.StabilityPct, r.AdoptionDetected, r.ContentionDays, r.RecommendationAgeHrs,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			if IsPartitionMissing(err) {
				logging.GetLogger().Errorf("WriteGPUMIGQuality: missing partition for gpu_mig_recommendation_quality: %v", err)
				return fmt.Errorf("partition missing for gpu_mig_recommendation_quality: %w", err)
			}
			return fmt.Errorf("WriteGPUMIGQuality batch exec: %w", err)
		}
	}
	return nil
}

// BuildGPUMIGQualityRows computes quality metrics for GPU MIG recommendations
// by comparing new recommendations against old ones and checking contention.
func BuildGPUMIGQualityRows(
	ctx context.Context, pool *pgxpool.Pool,
	orgID, clusterUUID string,
	newRecs map[GPUContainerKey][]*GPURec,
	oldRecs map[GPUContainerKey]OldGPUMIGRecommendation,
	currentProfiles map[GPUContainerKey]string,
) []GPUMIGQualityRow {
	if len(newRecs) == 0 {
		return nil
	}

	nowClock := time.Now().UTC()
	measuredAt := time.Date(nowClock.Year(), nowClock.Month(), nowClock.Day(), 0, 0, 0, 0, time.UTC)
	since := nowClock.AddDate(0, 0, -14)

	type qk struct {
		key    GPUContainerKey
		engine string
	}
	seen := map[qk]bool{}
	var rows []GPUMIGQualityRow

	for containerKey, recs := range newRecs {
		ns, wl, cn := containerKey.Namespace, containerKey.Workload, containerKey.ContainerName

		for _, rec := range recs {
			if rec.RecommendedGPUProfile == "" {
				continue
			}
			engine := "cost"
			key := GPUContainerKey{Namespace: ns, Workload: wl, ContainerName: cn}
			k := qk{key: key, engine: engine}
			if seen[k] {
				continue
			}
			seen[k] = true

			var stabilityPct float32 = 1.0
			var adopted bool
			var ageHours int64

			if old, ok := oldRecs[key]; ok {
				stabilityPct = ComputeGPUMIGStability(old.RecommendedGPUProfile, rec.RecommendedGPUProfile)
				currentProfile := currentProfiles[key]
				adopted = DetectGPUMIGAdoption(currentProfile, old.RecommendedGPUProfile)
				ageHours = ComputeRecommendationAgeHours(old.UpdatedAt, nowClock)
			}

		var contentionDays int64
		if pool != nil {
			cnt, err := CountGPUContentionDays(ctx, pool, clusterUUID, ns, wl, cn, since)
			if err == nil {
				contentionDays = cnt
			}
		}

			rows = append(rows, GPUMIGQualityRow{
				MeasuredAt:           measuredAt,
				OrgID:                orgID,
				ClusterUUID:          clusterUUID,
				Namespace:            ns,
				Workload:             wl,
				ContainerName:        cn,
				Engine:               engine,
				StabilityPct:         stabilityPct,
				AdoptionDetected:     adopted,
				ContentionDays:       contentionDays,
				RecommendationAgeHrs: ageHours,
			})
		}
	}
	return rows
}

