package engine

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	log "github.com/sirupsen/logrus"
)

// OldRecommendation holds previous recommendation values read from
// recommendation_sets before WriteRecommendations overwrites them.
type OldRecommendation struct {
	RecCPURequestMC  int64
	RecMemRequestKiB int64
	UpdatedAt        time.Time
}

// ReadOldRecommendations fetches the current recommendation_sets rows for the
// given containers (short_term/cost only), returning a map keyed by container.
// This must be called BEFORE WriteRecommendations to capture values for
// stability_pct and adoption_detected.
func ReadOldRecommendations(
	ctx context.Context, pool *pgxpool.Pool,
	orgID, clusterUUID string,
	keys []containerKey,
) (map[containerKey]OldRecommendation, error) {
	result := make(map[containerKey]OldRecommendation, len(keys))
	if len(keys) == 0 {
		return result, nil
	}

	rows, err := pool.Query(ctx, `
		SELECT namespace, workload, container_name,
			rec_cpu_request_millicores, rec_memory_request_kib, updated_at
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND term = 'short' AND engine = 'cost'`,
		orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("ReadOldRecommendations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ns, wl, cn string
		var old OldRecommendation
		if err := rows.Scan(&ns, &wl, &cn, &old.RecCPURequestMC, &old.RecMemRequestKiB, &old.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ReadOldRecommendations scan: %w", err)
		}
		result[containerKey{Namespace: ns, Workload: wl, ContainerName: cn}] = old
	}
	return result, rows.Err()
}

// ComputeStabilityPct calculates recommendation stability as:
//
//	max(0, 1.0 - |cpuVariation|/100*0.5 - |memVariation|/100*0.5)
//
// A score of 1.0 means no change; 0.0 means one or both resources changed 100%+.
func ComputeStabilityPct(cpuVariationPct, memVariationPct float32) float32 {
	v := 1.0 - math.Abs(float64(cpuVariationPct))/100*0.5 - math.Abs(float64(memVariationPct))/100*0.5
	if v < 0 {
		return 0
	}
	return float32(v)
}

// DetectAdoption returns true if current resource config matches the old
// recommendation within a 5% tolerance.
func DetectAdoption(currentCPUMC, currentMemKiB, recCPUMC, recMemKiB int64) bool {
	return withinTolerance(currentCPUMC, recCPUMC, 0.05) &&
		withinTolerance(currentMemKiB, recMemKiB, 0.05)
}

func withinTolerance(actual, expected int64, pct float64) bool {
	if expected == 0 {
		return actual == 0
	}
	delta := math.Abs(float64(actual)-float64(expected)) / float64(expected)
	return delta <= pct
}

// ComputeRecommendationAgeHours returns truncated integer hours since updatedAt.
func ComputeRecommendationAgeHours(updatedAt time.Time, now time.Time) int64 {
	if updatedAt.IsZero() {
		return 0
	}
	return int64(now.Sub(updatedAt).Hours())
}

// WriteRecommendationQuality batch-inserts quality metrics into recommendation_quality.
// It deduplicates recs by container (uses the first short_term/cost entry as representative).
func WriteRecommendationQuality(
	ctx context.Context, pool *pgxpool.Pool,
	newRecs []ContainerRec,
	oldRecs map[containerKey]OldRecommendation,
	oomCountsByContainer map[containerKey]int64,
) error {
	if len(newRecs) == 0 {
		return nil
	}

	now := time.Now().UTC()
	seen := map[containerKey]bool{}
	batch := &pgx.Batch{}

	for _, r := range newRecs {
		key := containerKey{
			Namespace:     r.Namespace,
			Workload:      r.Workload,
			ContainerName: r.ContainerName,
		}
		if seen[key] {
			continue
		}
		if r.Term != "short" || r.Engine != "cost" {
			continue
		}
		seen[key] = true

		var stabilityPct float32
		var adopted bool
		var ageHours int64

		if old, ok := oldRecs[key]; ok {
			cpuVar := computeVariation(old.RecCPURequestMC, r.RecCPURequestMC)
			memVar := computeVariation(old.RecMemRequestKiB, r.RecMemRequestKiB)
			stabilityPct = ComputeStabilityPct(cpuVar, memVar)
			adopted = DetectAdoption(r.CurrentCPURequestMC, r.CurrentMemRequestKiB, old.RecCPURequestMC, old.RecMemRequestKiB)
			ageHours = ComputeRecommendationAgeHours(old.UpdatedAt, now)
		} else {
			stabilityPct = 1.0
		}

		oomEventsAfter := oomCountsByContainer[key]

		batch.Queue(`
			INSERT INTO recommendation_quality (
				measured_at, org_id, cluster_uuid, namespace, workload, container_name,
				oom_events_after_rec, stability_pct, adoption_detected, recommendation_age_hours
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, container_name, measured_at)
			DO UPDATE SET
				oom_events_after_rec = EXCLUDED.oom_events_after_rec,
				stability_pct = EXCLUDED.stability_pct,
				adoption_detected = EXCLUDED.adoption_detected,
				recommendation_age_hours = EXCLUDED.recommendation_age_hours`,
			now, r.OrgID, r.ClusterUUID, r.Namespace, r.Workload, r.ContainerName,
			oomEventsAfter, stabilityPct, adopted, ageHours,
		)
	}

	if batch.Len() == 0 {
		return nil
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			if isPartitionMissing(err) {
				log.Errorf("WriteRecommendationQuality: missing partition for recommendation_quality: %v", err)
				return fmt.Errorf("partition missing for recommendation_quality: %w", err)
			}
			return fmt.Errorf("WriteRecommendationQuality batch exec: %w", err)
		}
	}
	return nil
}

func isPartitionMissing(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no partition")
}

// EnsureQualityPartitions creates monthly partitions for recommendation_quality
// covering the current month plus the next 2 months. This is idempotent.
// Partition DDL failures are non-fatal (log warning, continue).
func EnsureQualityPartitions(ctx context.Context, pool *pgxpool.Pool) {
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, i, 0)
		monthEnd := monthStart.AddDate(0, 1, 0)
		partName := fmt.Sprintf("recommendation_quality_%s", monthStart.Format("200601"))

		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF recommendation_quality FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			monthStart.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, sql); err != nil {
			log.Warnf("EnsureQualityPartitions: %s: %v (non-fatal)", partName, err)
		}
	}
}

// ContainerKeys extracts unique container keys from a set of ContainerRecs.
func ContainerKeys(recs []ContainerRec) []containerKey {
	seen := map[containerKey]bool{}
	var keys []containerKey
	for _, r := range recs {
		key := containerKey{
			Namespace:     r.Namespace,
			Workload:      r.Workload,
			ContainerName: r.ContainerName,
		}
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

// OOMCountsByContainer builds a map of container key -> total OOM count from recs.
func OOMCountsByContainer(recs []ContainerRec) map[containerKey]int64 {
	result := map[containerKey]int64{}
	for _, r := range recs {
		key := containerKey{
			Namespace:     r.Namespace,
			Workload:      r.Workload,
			ContainerName: r.ContainerName,
		}
		if _, ok := result[key]; !ok {
			result[key] = r.OOMCountSum
		}
	}
	return result
}
