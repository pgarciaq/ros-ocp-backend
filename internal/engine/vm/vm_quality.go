package vm

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	rootengine "github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// vmQualityKey uniquely identifies a VM within a cluster.
type vmQualityKey struct {
	Namespace string
	VMName    string
}

// OldVMRecommendation holds previous recommendation values read from
// vm_recommendations before PersistVMRecommendations overwrites them.
type OldVMRecommendation struct {
	RecommendedVCPU      int32
	RecommendedMemoryGiB int32
	UpdatedAt            time.Time
}

// ReadClusterOldVMRecommendations fetches existing vm_recommendations rows
// for a cluster (short term, cost engine) before they are overwritten.
func ReadClusterOldVMRecommendations(
	ctx context.Context, pool *pgxpool.Pool,
	orgID, clusterUUID string,
) (map[vmQualityKey]OldVMRecommendation, error) {
	rows, err := pool.Query(ctx, `
		SELECT namespace, vm_name,
			recommended_vcpu, recommended_memory_gib, updated_at
		FROM vm_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2 AND term = 'short_term' AND engine = 'cost'`,
		orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("ReadClusterOldVMRecommendations: %w", err)
	}
	defer rows.Close()

	result := make(map[vmQualityKey]OldVMRecommendation, 64)
	for rows.Next() {
		var ns, vm string
		var old OldVMRecommendation
		if err := rows.Scan(&ns, &vm, &old.RecommendedVCPU, &old.RecommendedMemoryGiB, &old.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ReadClusterOldVMRecommendations scan: %w", err)
		}
		result[vmQualityKey{Namespace: ns, VMName: vm}] = old
	}
	return result, rows.Err()
}

// ComputeVMStability compares old vs new recommended vCPU and memory.
// stability = max(0, 1.0 - |cpuVar|/100*0.5 - |memVar|/100*0.5)
func ComputeVMStability(oldRecVCPU, newRecVCPU, oldRecMemGiB, newRecMemGiB int32) float32 {
	cpuVar := computeInt32Variation(oldRecVCPU, newRecVCPU)
	memVar := computeInt32Variation(oldRecMemGiB, newRecMemGiB)
	v := 1.0 - math.Abs(float64(cpuVar))/100*0.5 - math.Abs(float64(memVar))/100*0.5
	if v < 0 {
		return 0
	}
	return float32(v)
}

func computeInt32Variation(current, recommended int32) float64 {
	if current == 0 {
		return 0
	}
	return float64(recommended-current) / float64(current) * 100
}

// DetectVMAdoption returns true if current vCPU and memory ≈ old recommended
// values within a 5% tolerance.
func DetectVMAdoption(currentVCPU, oldRecVCPU, currentMemGiB, oldRecMemGiB int32) bool {
	return rootengine.WithinTolerance(int64(currentVCPU), int64(oldRecVCPU), 0.05) &&
		rootengine.WithinTolerance(int64(currentMemGiB), int64(oldRecMemGiB), 0.05)
}

// CountVMSaturationDays counts days where CPU or memory utilization > 95% of
// allocated from daily VM digests.
func CountVMSaturationDays(digests []Digest) int64 {
	var count int64
	for _, d := range digests {
		cpuSaturated := d.CPURequestMC > 0 && d.CPUUsageP95MC > 0 &&
			float64(d.CPUUsageP95MC)/float64(d.CPURequestMC) > 0.95
		memSaturated := d.MemRequestKiB > 0 && d.MemUsageP95KiB > 0 &&
			float64(d.MemUsageP95KiB)/float64(d.MemRequestKiB) > 0.95
		if cpuSaturated || memSaturated {
			count++
		}
	}
	return count
}

// VMQualityRow represents a row to be inserted into vm_recommendation_quality.
type VMQualityRow struct {
	MeasuredAt           time.Time
	OrgID                string
	ClusterUUID          string
	Namespace            string
	VMName               string
	Engine               string
	StabilityPct         float32
	AdoptionDetected     bool
	SaturationDays       int64
	RecommendationAgeHrs int64
}

// WriteVMQuality batch-inserts quality metrics into vm_recommendation_quality.
func WriteVMQuality(ctx context.Context, pool *pgxpool.Pool, qualityRows []VMQualityRow) error {
	if len(qualityRows) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, r := range qualityRows {
		batch.Queue(`
			INSERT INTO vm_recommendation_quality (
				measured_at, org_id, cluster_uuid, namespace, vm_name, engine,
				stability_pct, adoption_detected, saturation_days, recommendation_age_hours
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (org_id, cluster_uuid, namespace, vm_name, engine, measured_at)
			DO UPDATE SET
				stability_pct = EXCLUDED.stability_pct,
				adoption_detected = EXCLUDED.adoption_detected,
				saturation_days = EXCLUDED.saturation_days,
				recommendation_age_hours = EXCLUDED.recommendation_age_hours`,
			r.MeasuredAt, r.OrgID, r.ClusterUUID, r.Namespace, r.VMName, r.Engine,
			r.StabilityPct, r.AdoptionDetected, r.SaturationDays, r.RecommendationAgeHrs,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			if rootengine.IsPartitionMissing(err) {
				logging.GetLogger().Errorf("WriteVMQuality: missing partition for vm_recommendation_quality: %v", err)
				return fmt.Errorf("partition missing for vm_recommendation_quality: %w", err)
			}
			return fmt.Errorf("WriteVMQuality batch exec: %w", err)
		}
	}
	return nil
}

// BuildVMQualityRows computes quality metrics for a set of VM recommendations
// by comparing them against old recommendations and digests.
func BuildVMQualityRows(
	recs []Recommendation,
	oldRecs map[vmQualityKey]OldVMRecommendation,
	digestsByVM map[vmQualityKey][]Digest,
) []VMQualityRow {
	if len(recs) == 0 {
		return nil
	}

	nowClock := time.Now().UTC()
	measuredAt := time.Date(nowClock.Year(), nowClock.Month(), nowClock.Day(), 0, 0, 0, 0, time.UTC)

	type qk struct {
		key    vmQualityKey
		engine string
	}
	seen := map[qk]bool{}
	var rows []VMQualityRow

	for _, r := range recs {
		if r.Engine != "cost" && r.Engine != "performance" {
			continue
		}
		key := vmQualityKey{Namespace: r.Namespace, VMName: r.VMName}
		k := qk{key: key, engine: r.Engine}
		if seen[k] {
			continue
		}
		seen[k] = true

		var stabilityPct float32 = 1.0
		var adopted bool
		var ageHours int64

		if old, ok := oldRecs[key]; ok {
			stabilityPct = ComputeVMStability(old.RecommendedVCPU, r.RecommendedVCPU, old.RecommendedMemoryGiB, r.RecommendedMemoryGiB)
			adopted = DetectVMAdoption(r.CurrentVCPU, old.RecommendedVCPU, r.CurrentMemoryGiB, old.RecommendedMemoryGiB)
			ageHours = rootengine.ComputeRecommendationAgeHours(old.UpdatedAt, nowClock)
		}

		var satDays int64
		if digests, ok := digestsByVM[key]; ok {
			satDays = CountVMSaturationDays(digests)
		}

		rows = append(rows, VMQualityRow{
			MeasuredAt:           measuredAt,
			OrgID:                r.OrgID,
			ClusterUUID:          r.ClusterUUID.String(),
			Namespace:            r.Namespace,
			VMName:               r.VMName,
			Engine:               r.Engine,
			StabilityPct:         stabilityPct,
			AdoptionDetected:     adopted,
			SaturationDays:       satDays,
			RecommendationAgeHrs: ageHours,
		})
	}
	return rows
}
