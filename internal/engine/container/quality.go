package container

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

var qualityPartitionMissing = promauto.NewCounter(prometheus.CounterOpts{
	Name: "rosocp_quality_partition_missing_total",
	Help: "Number of recommendation_quality writes that failed due to missing partition",
})

var (
	QualityOOMRate = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "ros_recommendation_oom_rate",
			Help:    "Mean OOM events after recommendation per quality write batch (0+); per-org/cluster detail in structured logs",
			Buckets: []float64{0, 1, 2, 5, 10, 25, 50, 100},
		},
	)
	QualityStability = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "ros_recommendation_stability",
			Help:    "Mean recommendation stability percentage per quality write batch (0-100); per-org/cluster detail in structured logs",
			Buckets: []float64{0, 25, 50, 75, 90, 95, 99, 100},
		},
	)
	QualityAdoptionRate = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "ros_recommendation_adoption_rate",
			Help:    "Mean adoption rate percentage per quality write batch (0-100); per-org/cluster detail in structured logs",
			Buckets: []float64{0, 25, 50, 75, 90, 95, 99, 100},
		},
	)
)

// OldRecommendation holds previous recommendation values read from
// recommendation_sets before WriteRecommendations overwrites them.
type OldRecommendation struct {
	RecCPURequestMC  int64
	RecMemRequestKiB int64
	UpdatedAt        time.Time
}

// ReadClusterOldRecommendations loads existing short-term recommendations for one
// engine on a cluster in a single query.
func ReadClusterOldRecommendations(
	ctx context.Context, pool *pgxpool.Pool,
	orgID, clusterUUID, engine string,
) (map[core.ContainerKey]OldRecommendation, error) {
	if engine != "cost" && engine != "performance" {
		return nil, fmt.Errorf("ReadClusterOldRecommendations: invalid engine %q", engine)
	}
	rows, err := pool.Query(ctx, `
		SELECT namespace, workload, COALESCE(workload_type, ''), container_name,
			COALESCE(rec_cpu_request_millicores, 0), COALESCE(rec_memory_request_kib, 0), updated_at
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND term = 'short' AND engine = $3`,
		orgID, clusterUUID, engine)
	if err != nil {
		return nil, fmt.Errorf("ReadClusterOldRecommendations: %w", err)
	}
	defer rows.Close()

	result := make(map[core.ContainerKey]OldRecommendation, 256)
	for rows.Next() {
		var ns, wl, wlType, cn string
		var old OldRecommendation
		if err := rows.Scan(&ns, &wl, &wlType, &cn, &old.RecCPURequestMC, &old.RecMemRequestKiB, &old.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ReadClusterOldRecommendations scan: %w", err)
		}
		result[core.ContainerKey{Namespace: ns, Workload: wl, WorkloadType: wlType, ContainerName: cn}] = old
	}
	return result, rows.Err()
}

// ReadOldRecommendations loads old recommendations for a specific set of container keys.
func ReadOldRecommendations(
	ctx context.Context, pool *pgxpool.Pool,
	orgID, clusterUUID, engine string,
	keys []core.ContainerKey,
) (map[core.ContainerKey]OldRecommendation, error) {
	result := make(map[core.ContainerKey]OldRecommendation, len(keys))
	if len(keys) == 0 {
		return result, nil
	}

	var sb strings.Builder
	if engine != "cost" && engine != "performance" {
		return nil, fmt.Errorf("ReadOldRecommendations: invalid engine %q", engine)
	}
	args := []any{orgID, clusterUUID, engine}
	sb.WriteString(`
		SELECT namespace, workload, COALESCE(workload_type, ''), container_name,
			COALESCE(rec_cpu_request_millicores, 0), COALESCE(rec_memory_request_kib, 0), updated_at
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND term = 'short' AND engine = $3
			AND (namespace, workload, container_name) IN (`)
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(",")
		}
		base := 4 + i*3
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
		result[core.ContainerKey{Namespace: ns, Workload: wl, WorkloadType: wlType, ContainerName: cn}] = old
	}
	return result, rows.Err()
}

// ReadClusterOldRecommendationsByEngine loads short-term old recommendations for both engines.
func ReadClusterOldRecommendationsByEngine(
	ctx context.Context, pool *pgxpool.Pool,
	orgID, clusterUUID string,
) (map[string]map[core.ContainerKey]OldRecommendation, error) {
	cost, err := ReadClusterOldRecommendations(ctx, pool, orgID, clusterUUID, "cost")
	if err != nil {
		return nil, err
	}
	performance, err := ReadClusterOldRecommendations(ctx, pool, orgID, clusterUUID, "performance")
	if err != nil {
		return nil, err
	}
	return map[string]map[core.ContainerKey]OldRecommendation{
		"cost":        cost,
		"performance": performance,
	}, nil
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
	return core.WithinTolerance(currentCPUMC, recCPUMC, 0.05) &&
		core.WithinTolerance(currentMemKiB, recMemKiB, 0.05)
}

// WriteRecommendationQuality batch-inserts quality metrics into recommendation_quality
// for each container × engine (cost and performance).
func WriteRecommendationQuality(
	ctx context.Context, pool *pgxpool.Pool,
	newRecs []core.ContainerRec,
	oldRecsByEngine map[string]map[core.ContainerKey]OldRecommendation,
	oomCountsByContainer map[core.ContainerKey]int64,
) error {
	if len(newRecs) == 0 {
		return nil
	}

	nowClock := time.Now().UTC()
	measuredAt := time.Date(nowClock.Year(), nowClock.Month(), nowClock.Day(), 0, 0, 0, 0, time.UTC)
	type qualityKey struct {
		key    core.ContainerKey
		engine string
	}
	seen := map[qualityKey]bool{}
	clusterAggs := map[qualityClusterAggKey]*qualityClusterAgg{}
	batch := &pgx.Batch{}

	for _, r := range newRecs {
		if r.Engine != "cost" && r.Engine != "performance" {
			continue
		}
		key := core.ContainerKey{
			Namespace:     r.Namespace,
			Workload:      r.Workload,
			WorkloadType:  r.WorkloadType,
			ContainerName: r.ContainerName,
		}
		qk := qualityKey{key: key, engine: r.Engine}
		if seen[qk] {
			continue
		}
		seen[qk] = true

		var stabilityPct float32
		var adopted bool
		var ageHours int64

		oldRecs := oldRecsByEngine[r.Engine]
		if oldRecs == nil {
			oldRecs = map[core.ContainerKey]OldRecommendation{}
		}
		if old, ok := oldRecs[key]; ok {
			cpuVar := core.ComputeVariation(old.RecCPURequestMC, r.RecCPURequestMC)
			memVar := core.ComputeVariation(old.RecMemRequestKiB, r.RecMemRequestKiB)
			stabilityPct = ComputeStabilityPct(cpuVar, memVar)
			adopted = DetectAdoption(r.CurrentCPURequestMC, r.CurrentMemRequestKiB, old.RecCPURequestMC, old.RecMemRequestKiB)
			ageHours = core.ComputeRecommendationAgeHours(old.UpdatedAt, nowClock)
		} else {
			stabilityPct = 1.0
		}

		oomEventsAfter := oomCountsByContainer[key]

		ck := qualityClusterAggKey{orgID: r.OrgID, clusterUUID: r.ClusterUUID}
		agg := clusterAggs[ck]
		if agg == nil {
			agg = &qualityClusterAgg{}
			clusterAggs[ck] = agg
		}
		agg.stabilitySum += float64(stabilityPct)
		if adopted {
			agg.adopted++
		}
		agg.oomSum += float64(oomEventsAfter)
		agg.n++

		batch.Queue(`
			INSERT INTO recommendation_quality (
				measured_at, org_id, cluster_uuid, namespace, workload, workload_type, container_name, engine,
				oom_events_after_rec, stability_pct, adoption_detected, recommendation_age_hours
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, engine, measured_at)
			DO UPDATE SET
				oom_events_after_rec = EXCLUDED.oom_events_after_rec,
				stability_pct = EXCLUDED.stability_pct,
				adoption_detected = EXCLUDED.adoption_detected,
				recommendation_age_hours = EXCLUDED.recommendation_age_hours`,
			measuredAt, r.OrgID, r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.ContainerName, r.Engine,
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
			if core.IsPartitionMissing(err) {
				qualityPartitionMissing.Inc()
				logging.GetLogger().Errorf("WriteRecommendationQuality: missing partition for recommendation_quality: %v", err)
				return fmt.Errorf("partition missing for recommendation_quality: %w", err)
			}
			return fmt.Errorf("WriteRecommendationQuality batch exec: %w", err)
		}
	}

	emitQualityGaugeMetrics(clusterAggs)

	return nil
}

type qualityClusterAggKey struct {
	orgID       string
	clusterUUID string
}

type qualityClusterAgg struct {
	stabilitySum float64
	adopted      int
	oomSum       float64
	n            int
}

func emitQualityGaugeMetrics(clusterAggs map[qualityClusterAggKey]*qualityClusterAgg) {
	log := logging.GetLogger()
	for key, agg := range clusterAggs {
		if agg == nil || agg.n == 0 {
			continue
		}
		n := float64(agg.n)
		stabilityPct := agg.stabilitySum / n * 100
		adoptionPct := float64(agg.adopted) / n * 100
		oomRate := agg.oomSum / n
		QualityStability.Observe(stabilityPct)
		QualityAdoptionRate.Observe(adoptionPct)
		QualityOOMRate.Observe(oomRate)
		log.WithFields(map[string]interface{}{
			"msg":               "recommendation quality batch aggregate",
			"org_id":            key.orgID,
			"cluster_uuid":      key.clusterUUID,
			"stability_pct":     stabilityPct,
			"adoption_rate_pct": adoptionPct,
			"oom_rate":          oomRate,
			"quality_rows":      agg.n,
		}).Info("recommendation quality batch aggregate")
	}
}

// ContainerKeys extracts unique container keys from a set of ContainerRecs.
func ContainerKeys(recs []core.ContainerRec) []core.ContainerKey {
	seen := map[core.ContainerKey]bool{}
	var keys []core.ContainerKey
	for _, r := range recs {
		key := core.ContainerKey{
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
func OOMCountsByContainer(recs []core.ContainerRec) map[core.ContainerKey]int64 {
	result := map[core.ContainerKey]int64{}
	for _, r := range recs {
		key := core.ContainerKey{
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
