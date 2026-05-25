package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

var qualityPartitionMissing = promauto.NewCounter(prometheus.CounterOpts{
	Name: "rosocp_quality_partition_missing_total",
	Help: "Number of recommendation_quality writes that failed due to missing partition",
})

// OldRecommendation holds previous recommendation values read from
// recommendation_sets before WriteRecommendations overwrites them.
type OldRecommendation struct {
	RecCPURequestMC  int64
	RecMemRequestKiB int64
	UpdatedAt        time.Time
}

// ReadOldRecommendations fetches the current recommendation_sets rows for the
// given containers (term='short', engine='cost' only), returning a map keyed
// by container. This must be called BEFORE WriteRecommendations to capture
// values for stability_pct and adoption_detected.
// ReadClusterOldRecommendations loads all existing short-term/cost recommendations
// for a cluster in a single query. Used by the streaming pipeline to avoid
// building O(containers) tuple lists.
func ReadClusterOldRecommendations(
	ctx context.Context, pool *pgxpool.Pool,
	orgID, clusterUUID string,
) (map[containerKey]OldRecommendation, error) {
	rows, err := pool.Query(ctx, `
		SELECT namespace, workload, COALESCE(workload_type, ''), container_name,
			COALESCE(rec_cpu_request_millicores, 0), COALESCE(rec_memory_request_kib, 0), updated_at
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND term = 'short' AND engine = 'cost'`,
		orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("ReadClusterOldRecommendations: %w", err)
	}
	defer rows.Close()

	result := make(map[containerKey]OldRecommendation, 256)
	for rows.Next() {
		var ns, wl, wlType, cn string
		var old OldRecommendation
		if err := rows.Scan(&ns, &wl, &wlType, &cn, &old.RecCPURequestMC, &old.RecMemRequestKiB, &old.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ReadClusterOldRecommendations scan: %w", err)
		}
		result[containerKey{Namespace: ns, Workload: wl, WorkloadType: wlType, ContainerName: cn}] = old
	}
	return result, rows.Err()
}

// ReadOldRecommendations loads old recommendations for a specific set of container keys.
// Retained for backward compatibility with tests.
func ReadOldRecommendations(
	ctx context.Context, pool *pgxpool.Pool,
	orgID, clusterUUID string,
	keys []containerKey,
) (map[containerKey]OldRecommendation, error) {
	result := make(map[containerKey]OldRecommendation, len(keys))
	if len(keys) == 0 {
		return result, nil
	}

	var sb strings.Builder
	args := []any{orgID, clusterUUID}
	sb.WriteString(`
		SELECT namespace, workload, COALESCE(workload_type, ''), container_name,
			COALESCE(rec_cpu_request_millicores, 0), COALESCE(rec_memory_request_kib, 0), updated_at
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND term = 'short' AND engine = 'cost'
			AND (namespace, workload, container_name) IN (`)
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(",")
		}
		base := 3 + i*3
		fmt.Fprintf(&sb, "($%d,$%d,$%d)", base, base+1, base+2)
		args = append(args, k.Namespace, k.Workload, k.ContainerName)
	}
	sb.WriteString(")")

	rows, err := pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("ReadOldRecommendations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ns, wl, wlType, cn string
		var old OldRecommendation
		if err := rows.Scan(&ns, &wl, &wlType, &cn, &old.RecCPURequestMC, &old.RecMemRequestKiB, &old.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ReadOldRecommendations scan: %w", err)
		}
		result[containerKey{Namespace: ns, Workload: wl, WorkloadType: wlType, ContainerName: cn}] = old
	}
	return result, rows.Err()
}

// ComputeStabilityPct calculates recommendation stability as:
//
//	max(0, 1.0 - |cpuVariation|/100*0.5 - |memVariation|/100*0.5)
//
// A score of 1.0 means no change; 0.0 means one or both resources changed 100%+.
func ComputeStabilityPct(cpuVariationPct, memVariationPct int32) float32 {
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
// Returns 0 if updatedAt is zero or in the future (clock skew).
func ComputeRecommendationAgeHours(updatedAt time.Time, now time.Time) int64 {
	if updatedAt.IsZero() {
		return 0
	}
	hours := int64(now.Sub(updatedAt).Hours())
	if hours < 0 {
		return 0
	}
	return hours
}

// WriteRecommendationQuality batch-inserts quality metrics into recommendation_quality.
// It deduplicates recs by container (uses the first cost-engine entry as representative).
func WriteRecommendationQuality(
	ctx context.Context, pool *pgxpool.Pool,
	newRecs []ContainerRec,
	oldRecs map[containerKey]OldRecommendation,
	oomCountsByContainer map[containerKey]int64,
) error {
	if len(newRecs) == 0 {
		return nil
	}

	nowClock := time.Now().UTC()
	measuredAt := time.Date(nowClock.Year(), nowClock.Month(), nowClock.Day(), 0, 0, 0, 0, time.UTC)
	seen := map[containerKey]bool{}
	batch := &pgx.Batch{}

	for _, r := range newRecs {
		key := containerKey{
			Namespace:     r.Namespace,
			Workload:      r.Workload,
			WorkloadType:  r.WorkloadType,
			ContainerName: r.ContainerName,
		}
		if seen[key] {
			continue
		}
		if r.Engine != "cost" {
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
			ageHours = ComputeRecommendationAgeHours(old.UpdatedAt, nowClock)
		} else {
			stabilityPct = 1.0
		}

		oomEventsAfter := oomCountsByContainer[key]

		batch.Queue(`
			INSERT INTO recommendation_quality (
				measured_at, org_id, cluster_uuid, namespace, workload, workload_type, container_name,
				oom_events_after_rec, stability_pct, adoption_detected, recommendation_age_hours
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, measured_at)
			DO UPDATE SET
				oom_events_after_rec = EXCLUDED.oom_events_after_rec,
				stability_pct = EXCLUDED.stability_pct,
				adoption_detected = EXCLUDED.adoption_detected,
				recommendation_age_hours = EXCLUDED.recommendation_age_hours`,
			measuredAt, r.OrgID, r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.ContainerName,
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
				qualityPartitionMissing.Inc()
				logging.GetLogger().Errorf("WriteRecommendationQuality: missing partition for recommendation_quality: %v", err)
				return fmt.Errorf("partition missing for recommendation_quality: %w", err)
			}
			return fmt.Errorf("WriteRecommendationQuality batch exec: %w", err)
		}
	}
	return nil
}

func isPartitionMissing(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrPartitionMissing) || strings.Contains(err.Error(), "no partition")
}

// EnsureQualityPartitions creates monthly partitions for recommendation_quality
// covering the current month plus the next 2 months. This is idempotent.
// Partition DDL failures are non-fatal (log warning, continue).
func EnsureQualityPartitions(ctx context.Context, pool *pgxpool.Pool) {
	ensureQualityPartitions(ctx, pool)
}

// ContainerKeys extracts unique container keys from a set of ContainerRecs.
func ContainerKeys(recs []ContainerRec) []containerKey {
	seen := map[containerKey]bool{}
	var keys []containerKey
	for _, r := range recs {
		key := containerKey{
			Namespace:     r.Namespace,
			Workload:      r.Workload,
			WorkloadType:  r.WorkloadType,
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
			WorkloadType:  r.WorkloadType,
			ContainerName: r.ContainerName,
		}
		if _, ok := result[key]; !ok {
			result[key] = r.OOMCountSum
		}
	}
	return result
}
