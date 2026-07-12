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
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// pgxBatchSender matches *pgxpool.Pool and pgx.Tx for SendBatch.
type pgxBatchSender interface {
	SendBatch(context.Context, *pgx.Batch) pgx.BatchResults
}

type containerKey struct {
	Namespace     string
	Workload      string
	WorkloadType  string
	ContainerName string
}

// OOMConfig holds configurable OOM bump parameters, typically read from
// environment variables (ROS_OOM_BASE_BUMP, ROS_OOM_MAX_BUMP).
// Zero values cause DefaultMemoryConfig defaults to be used.
type OOMConfig struct {
	BaseBump float64
	MaxBump  float64
}

// streamBatchSize is the number of containers accumulated before emitting a batch (ADR-0171).
const streamBatchSize = 500

// digestRowWithKey pairs a DigestRow with its container identity for post-query processing.
type digestRowWithKey struct {
	Key containerKey
	Row DigestRow
}

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
) ([]digestRowWithKey, error) {
	tx, err := pool.Begin(ctx)
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
	result := make([]digestRowWithKey, 0, defaultDigestRowCapacity)
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
		result = append(result, digestRowWithKey{
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

// RecommendWorkloadsStreaming loads all digests for a cluster into memory, groups
// them by container exploiting the ORDER BY guarantee, and calls emit for every
// batch of ~streamBatchSize containers' worth of recommendations.
//
// Digest rows are buffered upfront (via loadDigestRows) so that the database
// connection is released before recommendation processing begins. This prevents
// TCP backpressure from causing statement_timeout failures on clusters with
// 3,000+ containers (see issue #263).
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
	notifThresholds := NotificationThresholdsFromSizing(sizingThresholds)
	idleCfg := LoadIdleConfig(ctx, pool, orgID)
	maxIdleWindowDays := 0
	for _, tc := range terms {
		if tc.WindowDays > maxIdleWindowDays {
			maxIdleWindowDays = tc.WindowDays
		}
	}

	maxRows := config.GetConfig().MaxDigestRowsPerCluster
	allRows, err := loadDigestRows(ctx, pool, orgID, clusterUUID, start, end, maxRows)
	if err != nil {
		return err
	}
	logging.GetLogger().Infof("loaded %d digest rows for recommendation (cluster %s)", len(allRows), clusterUUID)

	now := time.Now().UTC()
	stalenessThreshold := StalenessThreshold()
	clusterLastReported := loadClusterLastReportedAt(ctx, pool, orgID, clusterUUID)

	var currentKey containerKey
	var currentDigests []DigestRow
	var latestDigestRow DigestRow
	var hasLatestDigest bool
	batch := make([]ContainerRec, 0, streamBatchSize*6)
	containerCount := 0
	firstRow := true

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := emit(batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}

	processContainer := func(key containerKey, digests []DigestRow, latest DigestRow) {
		currentCPUReqMC := latest.CPURequestP50MC
		currentCPULimMC := latest.CPURequestP95MC
		currentMemReqKiB := latest.MemRequestP50KiB
		currentMemLimKiB := latest.MemRequestP95KiB
		stale := isStaleRecommendation(now, latest.BucketDate, clusterLastReported, stalenessThreshold)

		idleRows := digests
		if maxIdleWindowDays > 0 {
			idleLo, idleHi := windowBounds(digests, latest.BucketDate, maxIdleWindowDays)
			idleRows = digests[idleLo:idleHi]
		}
		idleResult := ClassifyIdleState(
			idleRows, currentCPUReqMC, currentMemReqKiB,
			key.WorkloadType, key.Namespace, idleCfg,
		)
		idleClassified := idleClassificationAuthoritative(
			idleCfg, key.WorkloadType, key.Namespace, idleRows,
		)

		for _, tc := range terms {
			winLo, winHi := windowBounds(digests, latest.BucketDate, tc.WindowDays)
			windowRows := digests[winLo:winHi]
			if len(windowRows) < tc.MinDataDays {
				continue
			}

			dataDays := len(windowRows)
			confidence := computeConfidence(dataDays, tc.MinDataDays, tc.WindowDays)
			oomTotal := sumOOMCounts(windowRows)
			pcMin, pcMax, pcAvg := aggregatePodCounts(windowRows)
			desiredReplicas, availableReplicas := latestReplicaCounts(windowRows)
			monStart := windowRows[0].BucketDate
			monEnd := windowRows[len(windowRows)-1].BucketDate

			cpuCfgCost := CPUConfigFromSizing(sizingThresholds, now, tc.DecayHalfLifeHours, "cost")
			cpuCfgPerf := CPUConfigFromSizing(sizingThresholds, now, tc.DecayHalfLifeHours, "performance")
			memCfgCost := MemoryConfigFromSizing(sizingThresholds, now, tc.DecayHalfLifeHours, oomCfg, "cost")
			memCfgPerf := MemoryConfigFromSizing(sizingThresholds, now, tc.DecayHalfLifeHours, oomCfg, "performance")

			for _, profile := range []string{"cost", "performance"} {
				var cpuCfg CPUConfig
				var memCfg MemoryConfig
				if profile == "performance" {
					cpuCfg = cpuCfgPerf
					memCfg = memCfgPerf
				} else {
					cpuCfg = cpuCfgCost
					memCfg = memCfgCost
				}
				memCfg.OOMCountSum = oomTotal
				if memCfg.OOMMaxBump < 1.0 {
					memCfg.OOMMaxBump = 1.0
				}

				cpuRec, memRec, expl := RecommendCPUAndMemory(windowRows, cpuCfg, memCfg)
				expl.DataDays = dataDays

			var isIdle bool
			if idleClassified {
				isIdle = idleResult.State == IdleStateIdle || idleResult.State == IdleStateZombie
			} else {
				isIdle = cpuRec.IsIdle
			}

				var recCPUReq, recCPULim, recMemReq, recMemLim int64
				if profile == "performance" {
					recCPUReq = cpuRec.PerfRequestMC
					recCPULim = cpuRec.PerfLimitMC
					recMemReq = memRec.PerfRequestKiB
					recMemLim = memRec.PerfLimitKiB
				} else {
					recCPUReq = cpuRec.CostRequestMC
					recCPULim = cpuRec.CostLimitMC
					recMemReq = memRec.CostRequestKiB
					recMemLim = memRec.CostLimitKiB
				}

				rec := ContainerRec{
					OrgID:                orgID,
					ClusterUUID:          clusterUUID,
					Namespace:            key.Namespace,
					Workload:             key.Workload,
					WorkloadType:         key.WorkloadType,
					ContainerName:        key.ContainerName,
					Term:                 tc.Name,
					Engine:               profile,
					RecCPURequestMC:      recCPUReq,
					RecCPULimitMC:        recCPULim,
					RecMemRequestKiB:     recMemReq,
					RecMemLimitKiB:       recMemLim,
					CurrentCPURequestMC:  currentCPUReqMC,
					CurrentCPULimitMC:    currentCPULimMC,
					CurrentMemRequestKiB: currentMemReqKiB,
					CurrentMemLimitKiB:   currentMemLimKiB,
					ConfidenceLevel:      confidence,
					CPUTrendSlope:        cpuRec.TrendSlope,
					MemTrendSlope:        memRec.TrendSlope,
				IsIdle:               isIdle,
				IsAbandoned:          false,
					IdleState:            idleResult.State,
					IdleSince:            idleResult.IdleSince,
					IdleDurationDays:     idleResult.DurationDays,
					PeakCPUMC:            idleResult.PeakCPUMC,
					PeakMemoryBytes:      idleResult.PeakMemoryBytes,
					OOMCountSum:          oomTotal,
					DataDays:             dataDays,
					Stale:                stale,
					PodCountMin:          pcMin,
					PodCountMax:          pcMax,
					PodCountAvg:          pcAvg,
					DesiredReplicas:      desiredReplicas,
					AvailableReplicas:    availableReplicas,
					MonitoringStartTime:  monStart,
					MonitoringEndTime:    monEnd,
					Expl:                 expl,
				}
		rec.VariationCPURequestPct = computeVariation(currentCPUReqMC, rec.RecCPURequestMC)
		rec.VariationCPULimitPct = computeVariation(currentCPULimMC, rec.RecCPULimitMC)
		rec.VariationMemRequestPct = computeVariation(currentMemReqKiB, rec.RecMemRequestKiB)
		rec.VariationMemLimitPct = computeVariation(currentMemLimKiB, rec.RecMemLimitKiB)
		rec.NotificationCodes = EvaluateNotificationsWithThresholds(rec, tc.MinDataDays, notifThresholds)

		if idleResult.State == IdleStateActive {
			rec.CategoryCPU = ClassifyResource(rec.VariationCPURequestPct)
			rec.CategoryMemory = ClassifyResource(rec.VariationMemRequestPct)
			rec.Category = ClassifyOverall(rec.CategoryCPU, rec.CategoryMemory)
		}

		ComputeRecommendedReplicas(&rec, tc.ReplicaTargetUtilizationPct, latest)

			batch = append(batch, rec)
			}
		}
	}

	for _, rk := range allRows {
		if !firstRow && rk.Key != currentKey {
			processContainer(currentKey, currentDigests, latestDigestRow)
			containerCount++
			currentDigests = currentDigests[:0]
			hasLatestDigest = false

			if containerCount%streamBatchSize == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := flush(); err != nil {
					return err
				}
			}
		}

		firstRow = false
		currentKey = rk.Key
		currentDigests = append(currentDigests, rk.Row)
		if !hasLatestDigest || rk.Row.BucketDate.After(latestDigestRow.BucketDate) {
			latestDigestRow = rk.Row
			hasLatestDigest = true
		}
	}

	if len(currentDigests) > 0 {
		processContainer(currentKey, currentDigests, latestDigestRow)
	}
	return flush()
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

func flushRecommendationBatch(ctx context.Context, sender pgxBatchSender, batch *pgx.Batch) error {
	n := batch.Len()
	br := sender.SendBatch(ctx, batch)
	defer br.Close()
	for i := range n {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("flushRecommendationBatch: statement %d/%d: %w", i+1, n, err)
		}
	}
	return nil
}

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
		if err := flushRecommendationBatch(ctx, tx, batch); err != nil {
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

// windowBounds returns start (inclusive) and end (exclusive) indices into rows
// for the last windowDays from endDate. Rows must be sorted by BucketDate
// (ascending) from the DB query. The caller slices rows[start:end] for a
// zero-copy view of the window.
func windowBounds(rows []DigestRow, endDate time.Time, windowDays int) (start, end int) {
	if len(rows) == 0 {
		return 0, 0
	}
	cutoffDay := endDate.AddDate(0, 0, -(windowDays - 1)).Truncate(24 * time.Hour)
	endDay := endDate.Truncate(24 * time.Hour)

	lo := 0
	hi := len(rows)
	for lo < hi {
		mid := (lo + hi) / 2
		if rows[mid].BucketDate.Before(cutoffDay) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	for i := lo; i < len(rows); i++ {
		if rows[i].BucketDate.After(endDay) {
			return lo, i
		}
	}
	return lo, len(rows)
}

// computeConfidence returns a 0.0-1.0 score based on data availability.
func computeConfidence(dataDays, minDataDays, windowDays int) float32 {
	if dataDays <= 0 {
		return 0
	}
	ratio := float32(dataDays) / float32(windowDays)
	if ratio > 1.0 {
		ratio = 1.0
	}
	return ratio
}

// computeVariation returns the percentage change from current to recommended,
// rounded to the nearest integer.
func computeVariation(current, rec int64) int32 {
	if current == 0 {
		return 0
	}
	diff := (rec - current) * 100
	if diff >= 0 {
		return int32((diff + current/2) / current)
	}
	return int32((diff - current/2) / current)
}

// aggregatePodCounts computes min-of-mins, max-of-maxes, and weighted average
// of per-day pod count values across the term window's digest rows.
func aggregatePodCounts(rows []DigestRow) (pcMin, pcMax, pcAvg int64) {
	if len(rows) == 0 {
		return 0, 0, 0
	}
	hasAny := false
	for _, r := range rows {
		if r.PodCountMin > 0 || r.PodCountMax > 0 || r.PodCountAvg > 0 {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return 0, 0, 0
	}
	first := true
	var sumAvg int64
	var count int
	for _, r := range rows {
		if r.PodCountMax == 0 && r.PodCountMin == 0 && r.PodCountAvg == 0 {
			continue
		}
		if first || r.PodCountMin < pcMin {
			pcMin = r.PodCountMin
		}
		if first || r.PodCountMax > pcMax {
			pcMax = r.PodCountMax
		}
		sumAvg += r.PodCountAvg
		count++
		first = false
	}
	if count > 0 {
		pcAvg = (sumAvg + int64(count)/2) / int64(count)
	}
	return
}

// latestReplicaCounts returns the desired and available replica counts from
// the most recent DigestRow that has a non-zero desired_replicas value.
func latestReplicaCounts(rows []DigestRow) (desired, available int64) {
	var latestDate time.Time
	for _, r := range rows {
		if r.DesiredReplicas > 0 && r.BucketDate.After(latestDate) {
			latestDate = r.BucketDate
			desired = r.DesiredReplicas
			available = r.AvailableReplicas
		}
	}
	return desired, available
}

func sumOOMCounts(rows []DigestRow) int64 {
	var total int64
	for _, r := range rows {
		total += r.OOMCountSum
	}
	return total
}

// DefaultStalenessThreshold is used when ROS_STALENESS_THRESHOLD_HOURS is not set.
const DefaultStalenessThreshold = 48 * time.Hour

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

// isStaleRecommendation marks a recommendation stale when the cluster has not
// reported within the threshold. Reship and delayed uploads refresh
// last_reported_at even when digest bucket_dates are historical, so cluster
// activity takes precedence over per-container digest age.
func isStaleRecommendation(now, latestDigestDate, clusterLastReported time.Time, threshold time.Duration) bool {
	if !clusterLastReported.IsZero() {
		return now.Sub(clusterLastReported) > threshold
	}
	return now.Sub(latestDigestDate.Truncate(24*time.Hour)) > threshold
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

// latestDigest returns the DigestRow with the most recent BucketDate.
func latestDigest(rows []DigestRow) DigestRow {
	if len(rows) == 0 {
		return DigestRow{}
	}
	best := rows[0]
	for _, r := range rows[1:] {
		if r.BucketDate.After(best.BucketDate) {
			best = r
		}
	}
	return best
}
