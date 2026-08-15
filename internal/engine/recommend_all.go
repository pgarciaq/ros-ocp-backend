package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/clustercache"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	db "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	libengine "github.com/redhatinsights/ros-ocp-backend/librobne/engine"
)

// PgxBatchSender is a type alias for db.PgxBatchSender.
type PgxBatchSender = db.PgxBatchSender

type containerKey = core.ContainerKey

// OOMConfig holds configurable OOM bump parameters, typically read from
// environment variables (ROS_OOM_BASE_BUMP, ROS_OOM_MAX_BUMP).
// Zero values cause DefaultMemoryConfig defaults to be used.
type OOMConfig struct {
	BaseBump float64
	MaxBump  float64
}

// streamBatchSize is the number of containers accumulated before emitting a batch (ADR-0171).
const streamBatchSize = 500

// ErrDigestRowCapExceeded is returned when loadDigestRows hits the configured
// ROS_MAX_DIGEST_ROWS_PER_CLUSTER limit. Callers should skip that cluster's
// recommendations rather than crash. The error message includes actionable
// guidance for operators.
var ErrDigestRowCapExceeded = fmt.Errorf("digest row cap exceeded")

// loadDigestRows fetches all digest rows for a cluster in a transaction with the
// ingest statement timeout. Rows are buffered in memory and the database connection
// is released before any recommendation processing begins. This avoids TCP
// backpressure timeouts that occur when long-running recommendation writes block
// the client from consuming a streaming result set (see issue #263).
//
// maxRows caps the number of rows buffered to prevent OOM on anomalous clusters
// (see issue #290). 0 means unlimited. Exceeding the cap is a hard error — the
// caller must skip that cluster rather than process truncated data.
func loadDigestRows(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
	maxRows int,
) ([]KeyedDigest, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin digest read tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := db.SetLocalIngestStatementTimeout(ctx, tx); err != nil {
		return nil, fmt.Errorf("set ingest statement timeout: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT bucket_date,
			COALESCE(cpu_request_p50_mc, 0), COALESCE(cpu_request_p60_mc, 0),
			COALESCE(cpu_request_p95_mc, 0), COALESCE(cpu_request_p98_mc, 0), COALESCE(cpu_request_p99_mc, 0),
			COALESCE(cpu_usage_p50_mc, 0), COALESCE(cpu_usage_p60_mc, 0),
			COALESCE(cpu_usage_p95_mc, 0), COALESCE(cpu_usage_p98_mc, 0), COALESCE(cpu_usage_p99_mc, 0),
			COALESCE(cpu_usage_max_mc, 0),
			COALESCE(cpu_throttle_p95_mc, 0), COALESCE(cpu_throttle_max_mc, 0),
			COALESCE(memory_request_p50_kib, 0), COALESCE(memory_request_p60_kib, 0),
			COALESCE(memory_request_p95_kib, 0), COALESCE(memory_request_p98_kib, 0), COALESCE(memory_request_p99_kib, 0),
			COALESCE(memory_usage_p50_kib, 0), COALESCE(memory_usage_p60_kib, 0),
			COALESCE(memory_usage_p95_kib, 0), COALESCE(memory_usage_p98_kib, 0), COALESCE(memory_usage_p99_kib, 0),
			COALESCE(memory_usage_max_kib, 0),
			COALESCE(memory_rss_p95_kib, 0), COALESCE(memory_rss_max_kib, 0),
			COALESCE(oom_count_sum, 0), COALESCE(cpu_usage_mean_mc, 0), COALESCE(memory_usage_mean_kib, 0),
			COALESCE(sample_count, 0),
			COALESCE(pod_count_min, 0), COALESCE(pod_count_max, 0), COALESCE(pod_count_avg, 0),
			COALESCE(desired_replicas, 0), COALESCE(available_replicas, 0),
			cpu_usage_cv_bp,
			namespace, workload, workload_type, container_name
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = 'all_hours'
		ORDER BY namespace, workload, workload_type, container_name, bucket_date`,
		orgID, clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("query digests: %w", err)
	}
	defer rows.Close()

	warnThreshold := 0
	if maxRows > 0 {
		warnThreshold = maxRows * 4 / 5 // 80% of cap
	}
	warnLogged := false

	const defaultDigestRowCapacity = 8192
	result := make([]KeyedDigest, 0, defaultDigestRowCapacity)
	for rows.Next() {
		var d DigestRow
		var ns, wl, wlType, cn string
		if err := rows.Scan(
			&d.BucketDate,
			&d.CPURequestP50MC, &d.CPURequestP60MC, &d.CPURequestP95MC, &d.CPURequestP98MC, &d.CPURequestP99MC,
			&d.CPUUsageP50MC, &d.CPUUsageP60MC, &d.CPUUsageP95MC, &d.CPUUsageP98MC, &d.CPUUsageP99MC, &d.CPUUsageMaxMC,
			&d.CPUThrottleP95MC, &d.CPUThrottleMaxMC,
			&d.MemRequestP50KiB, &d.MemRequestP60KiB,
			&d.MemRequestP95KiB, &d.MemRequestP98KiB, &d.MemRequestP99KiB,
			&d.MemUsageP50KiB, &d.MemUsageP60KiB,
			&d.MemUsageP95KiB, &d.MemUsageP98KiB, &d.MemUsageP99KiB,
			&d.MemUsageMaxKiB,
			&d.MemRSSP95KiB, &d.MemRSSMaxKiB,
			&d.OOMCountSum, &d.CPUUsageMeanMC, &d.MemUsageMeanKiB, &d.SampleCount,
			&d.PodCountMin, &d.PodCountMax, &d.PodCountAvg,
			&d.DesiredReplicas, &d.AvailableReplicas,
			&d.CPUUsageCVBP,
			&ns, &wl, &wlType, &cn,
		); err != nil {
			return nil, fmt.Errorf("scan digest row: %w", err)
		}
		result = append(result, KeyedDigest{
			Key: containerKey{Namespace: ns, Workload: wl, WorkloadType: wlType, ContainerName: cn},
			Row: d,
		})

		count := len(result)
		if maxRows > 0 {
			if count > maxRows {
				metrics.DigestRowsCapExceeded.Inc()
				return nil, fmt.Errorf("%w: loaded %d rows (cap=%d) for cluster %s — "+
					"reduce ROS_MAX_LOOKBACK_DAYS or increase ROS_MAX_DIGEST_ROWS_PER_CLUSTER",
					ErrDigestRowCapExceeded, count, maxRows, clusterUUID)
			}
			if !warnLogged && count >= warnThreshold {
				warnLogged = true
				metrics.DigestRowsCapWarning.Inc()
				logging.GetLogger().Warnf(
					"digest row count at 80%% of cap: org_id=%s cluster=%s count=%d cap=%d",
					orgID, clusterUUID, count, maxRows,
				)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate digest rows: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit digest read tx: %w", err)
	}

	return result, nil
}

// RecommendWorkloadsStreaming loads terms/thresholds/idle and digest rows from
// PostgreSQL, then calls RecommendWorkloads (no pool in the compute loop).
// emit is invoked every ~streamBatchSize containers.
func RecommendWorkloadsStreaming(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
	oomCfg OOMConfig,
	emit func([]ContainerRec) error,
) error {
	terms, err := LoadTermConfigCached(ctx, pool, orgID, "container")
	if err != nil {
		return fmt.Errorf("load term config: %w", err)
	}

	sizingThresholds, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	if err != nil {
		return fmt.Errorf("load container thresholds: %w", err)
	}
	idleCfg := LoadIdleConfig(ctx, pool, orgID)

	maxRows := config.GetConfig().MaxDigestRowsPerCluster
	allRows, err := loadDigestRows(ctx, pool, orgID, clusterUUID, start, end, maxRows)
	if err != nil {
		return err
	}
	logging.GetLogger().Infof("loaded %d digest rows for recommendation (cluster %s)", len(allRows), clusterUUID)

	cfg := EngineConfig{
		OrgID:               orgID,
		ClusterUUID:         clusterUUID,
		Terms:               terms,
		Sizing:              sizingThresholds,
		Idle:                idleCfg,
		OOMBaseBump:         oomCfg.BaseBump,
		OOMMaxBump:          oomCfg.MaxBump,
		Now:                 time.Now().UTC(),
		StalenessThreshold:  StalenessThreshold(),
		ClusterLastReported: loadClusterLastReportedAt(ctx, pool, orgID, clusterUUID),
		BatchSize:           streamBatchSize,
	}
	return RecommendWorkloads(ctx, allRows, cfg, emit)
}

// RecommendAllWorkloads is a convenience wrapper that collects all streaming results
// into a single slice. Prefer RecommendWorkloadsStreaming in production for bounded memory.
func RecommendAllWorkloads(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
	oomCfg OOMConfig,
) ([]ContainerRec, error) {
	var results []ContainerRec
	err := RecommendWorkloadsStreaming(ctx, pool, orgID, clusterUUID, start, end, oomCfg, func(batch []ContainerRec) error {
		results = append(results, batch...)
		return nil
	})
	return results, err
}

// FlushRecommendationBatch delegates to db.FlushRecommendationBatch.
var FlushRecommendationBatch = db.FlushRecommendationBatch

// WriteRecommendations batch-upserts ContainerRec results into recommendation_sets.
func WriteRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []ContainerRec) error {
	if len(recs) == 0 {
		return nil
	}

	t0 := time.Now()
	defer func() { metrics.ObserveDB("write_recommendations", t0) }()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for recommendations: %w", err)
	}
	defer tx.Rollback(ctx)

	for chunkStart := 0; chunkStart < len(recs); chunkStart += db.MaxPgxBatchQueue {
		chunkEnd := min(chunkStart+db.MaxPgxBatchQueue, len(recs))
		batch := &pgx.Batch{}
		for _, r := range recs[chunkStart:chunkEnd] {
			containerID := model.NativeContainerID(r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.ContainerName)
			batch.Queue(`
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			term, engine, container_id,
			rec_cpu_request_millicores, rec_cpu_limit_millicores,
			rec_memory_request_kib, rec_memory_limit_kib,
			current_cpu_request_millicores, current_cpu_limit_millicores,
			current_memory_request_kib, current_memory_limit_kib,
			variation_cpu_request_pct, variation_cpu_limit_pct,
			variation_memory_request_pct, variation_memory_limit_pct,
			notification_codes, confidence_level, stale,
			pod_count_min, pod_count_max, pod_count_avg,
			desired_replicas, available_replicas,
			recommended_replicas, replica_confidence, replica_explanation,
			estimated_savings_cents,
			estimated_cpu_savings_cents, estimated_memory_savings_cents,
			idle_state, idle_since, idle_duration_days,
			estimated_waste_cents, peak_cpu_millicores, peak_memory_bytes,
			monitoring_start_time, monitoring_end_time,
			category, category_cpu, category_memory,`+containerExplSQLColumns+`,
			updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41,$42,$43,$44,$45,$46,`+containerExplValuePlaceholders(47)+`,now())
		ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine)
		DO UPDATE SET
			rec_cpu_request_millicores = EXCLUDED.rec_cpu_request_millicores,
			rec_cpu_limit_millicores = EXCLUDED.rec_cpu_limit_millicores,
			rec_memory_request_kib = EXCLUDED.rec_memory_request_kib,
			rec_memory_limit_kib = EXCLUDED.rec_memory_limit_kib,
			current_cpu_request_millicores = EXCLUDED.current_cpu_request_millicores,
			current_cpu_limit_millicores = EXCLUDED.current_cpu_limit_millicores,
			current_memory_request_kib = EXCLUDED.current_memory_request_kib,
			current_memory_limit_kib = EXCLUDED.current_memory_limit_kib,
			variation_cpu_request_pct = EXCLUDED.variation_cpu_request_pct,
			variation_cpu_limit_pct = EXCLUDED.variation_cpu_limit_pct,
			variation_memory_request_pct = EXCLUDED.variation_memory_request_pct,
			variation_memory_limit_pct = EXCLUDED.variation_memory_limit_pct,
			notification_codes = EXCLUDED.notification_codes,
			confidence_level = EXCLUDED.confidence_level,
			stale = EXCLUDED.stale,
			pod_count_min = EXCLUDED.pod_count_min,
			pod_count_max = EXCLUDED.pod_count_max,
			pod_count_avg = EXCLUDED.pod_count_avg,
			desired_replicas = EXCLUDED.desired_replicas,
			available_replicas = EXCLUDED.available_replicas,
			recommended_replicas = EXCLUDED.recommended_replicas,
			replica_confidence = EXCLUDED.replica_confidence,
			replica_explanation = EXCLUDED.replica_explanation,
			estimated_savings_cents = EXCLUDED.estimated_savings_cents,
			estimated_cpu_savings_cents = EXCLUDED.estimated_cpu_savings_cents,
			estimated_memory_savings_cents = EXCLUDED.estimated_memory_savings_cents,
			idle_state = EXCLUDED.idle_state,
			idle_since = EXCLUDED.idle_since,
			idle_duration_days = EXCLUDED.idle_duration_days,
			estimated_waste_cents = EXCLUDED.estimated_waste_cents,
			peak_cpu_millicores = EXCLUDED.peak_cpu_millicores,
			peak_memory_bytes = EXCLUDED.peak_memory_bytes,
			monitoring_start_time = EXCLUDED.monitoring_start_time,
			monitoring_end_time = EXCLUDED.monitoring_end_time,
			container_id = EXCLUDED.container_id,
			category = EXCLUDED.category,
			category_cpu = EXCLUDED.category_cpu,
			category_memory = EXCLUDED.category_memory,`+containerExplUpdateSet+`,
			updated_at = now()`,
				appendContainerExplArgs([]any{
					r.OrgID, r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.ContainerName,
					r.Term, r.Engine, containerID,
					r.RecCPURequestMC, r.RecCPULimitMC,
					r.RecMemRequestKiB, r.RecMemLimitKiB,
					r.CurrentCPURequestMC, r.CurrentCPULimitMC,
					r.CurrentMemRequestKiB, r.CurrentMemLimitKiB,
					r.VariationCPURequestPct, r.VariationCPULimitPct,
					r.VariationMemRequestPct, r.VariationMemLimitPct,
					r.NotificationCodes, r.ConfidenceLevel, r.Stale,
					r.PodCountMin, r.PodCountMax, r.PodCountAvg,
					r.DesiredReplicas, r.AvailableReplicas,
					nullIfZeroInt64(r.RecommendedReplicas), nullIfEmpty(r.ReplicaConfidence), nullIfEmpty(r.ReplicaExplanation),
					r.EstimatedSavingsCents,
					r.EstimatedCPUSavingsCents, r.EstimatedMemSavingsCents,
					idleStateForWrite(r.IdleState), r.IdleSince, r.IdleDurationDays,
					r.EstimatedWasteCents, r.PeakCPUMC, r.PeakMemoryBytes,
					r.MonitoringStartTime, r.MonitoringEndTime,
					nullIfEmpty(r.Category), nullIfEmpty(r.CategoryCPU), nullIfEmpty(r.CategoryMemory),
				}, r.Expl)...,
			)
		}
		if err := FlushRecommendationBatch(ctx, tx, batch); err != nil {
			return fmt.Errorf("batch exec: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit recommendations tx: %w", err)
	}
	return nil
}

// WriteRecommendationsAndRefreshOrg persists recommendations and refreshes org metadata.
// Use for single-batch writes (tests, tooling). Streaming reconcile cycles should call
// WriteRecommendations per batch and RefreshOrgMetadata once at the end.
func WriteRecommendationsAndRefreshOrg(ctx context.Context, pool *pgxpool.Pool, recs []ContainerRec) error {
	if err := WriteRecommendations(ctx, pool, recs); err != nil {
		return err
	}
	if len(recs) == 0 {
		return nil
	}
	return RefreshOrgMetadata(ctx, pool, recs[0].OrgID)
}

// RefreshOrgMetadata updates org_container_keys and org_recommendation_stats for an org.
// Call once at the end of a reconcile cycle instead of after every write batch.
func RefreshOrgMetadata(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	if orgID == "" {
		return nil
	}
	if err := model.RefreshOrgContainerKeys(ctx, pool, orgID); err != nil {
		return err
	}
	if err := model.RefreshOrgRecommendationStats(ctx, pool, orgID); err != nil {
		return err
	}
	fleetsummary.InvalidateOrg(orgID)
	fleetheatmap.InvalidateOrg(orgID)
	clustercache.InvalidateOrg(orgID)
	return nil
}

var (
	windowBounds          = libengine.WindowBounds
	computeConfidence     = core.ComputeConfidence
	computeVariation      = core.ComputeVariation
	aggregatePodCounts    = libengine.AggregatePodCounts
	latestReplicaCounts   = libengine.LatestReplicaCounts
	sumOOMCounts          = libengine.SumOOMCounts
	isStaleRecommendation = libengine.IsStaleRecommendation
	latestDigest          = libengine.LatestDigest
)

// DefaultStalenessThreshold is used when ROS_STALENESS_THRESHOLD_HOURS is not set.
const DefaultStalenessThreshold = libengine.DefaultStalenessThreshold

// loadClusterLastReportedAt returns clusters.last_reported_at for org+cluster, or zero time if unknown.
func loadClusterLastReportedAt(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) time.Time {
	var ts time.Time
	err := pool.QueryRow(ctx, `
		SELECT c.last_reported_at
		FROM clusters c
		JOIN rh_accounts ra ON ra.id = c.tenant_id
		WHERE ra.org_id = $1 AND c.cluster_uuid = $2::uuid`,
		orgID, clusterUUID).Scan(&ts)
	if err != nil {
		return time.Time{}
	}
	return ts.UTC()
}

// StalenessThreshold returns the configured staleness threshold duration.
func StalenessThreshold() time.Duration {
	cfg := config.GetConfig()
	if cfg.StalenessThresholdHours > 0 {
		return time.Duration(cfg.StalenessThresholdHours) * time.Hour
	}
	return DefaultStalenessThreshold
}

// MarkUnreportedContainersStale marks recommendation_sets rows stale when their
// composite key no longer appears in the current digest data. This handles the
// case where a container's workload_type (or other key column) changes: the old
// row is never overwritten by the ON CONFLICT upsert (different key = new row),
// so without this sweep the old recommendation lingers with stale=false despite
// having no matching digest data.
//
// The mechanism relies on WriteRecommendations setting updated_at = now() for
// every row it upserts. After a full reconcile cycle, any non-stale row whose
// updated_at is older than cycleStart was not refreshed — its composite key has
// no matching digests.
//
// A 5-minute grace window accounts for clock skew and transaction commit delays.
func MarkUnreportedContainersStale(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, cycleStart time.Time) (int64, error) {
	grace := cycleStart.Add(-5 * time.Minute)
	tag, err := pool.Exec(ctx, `
		UPDATE recommendation_sets
		SET stale = true, updated_at = now()
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND stale = false
		  AND updated_at < $3`,
		orgID, clusterUUID, grace,
	)
	if err != nil {
		return 0, fmt.Errorf("mark unreported containers stale: %w", err)
	}
	return tag.RowsAffected(), nil
}
