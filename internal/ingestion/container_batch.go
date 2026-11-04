package ingestion

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

// #758 (V15-08): a manifest's container files must contribute to ONE daily
// distribution per container/day. The per-file stream computed and REPLACED
// the digest per key at each file's EOF, so files carrying disjoint portions
// of the same container/day clobbered each other (count-based producer splits
// do not enforce container/day boundaries).
//
// ContainerDigestBatch is the approved option-B hybrid: the manifest's files
// stream into SHARED accumulators (lossless — raw samples union), with
// finite staging thresholds bounding memory. Past a threshold, samples go
// to the per-manifest UNLOGGED staging table instead of partial digest
// upserts; Flush computes digests per key, streaming staged rows in chunks
// and merging any in-memory remainder. The standalone per-file path
// (ParseAndDigestCSV and friends) keeps today's semantics for its callers.

const (
	// defaultContainerStageKeyThreshold bounds the in-memory digest GROUPS of
	// a manifest batch before samples stage to disk. ~8k container-day groups
	// at 96-384 samples each is on the order of today's single max-size file
	// (~100MB CSV ≈ 600k samples) — the status quo ceiling, now enforced per
	// manifest. ROS_INGEST_FLUSH_BATCH_SIZE overrides it (an explicitly set
	// knob keeps its contract; the config layer's MaxInt32 default does not
	// count as "set").
	defaultContainerStageKeyThreshold = 8192
	// defaultContainerStageSampleThreshold bounds in-memory SAMPLES — the
	// key threshold alone cannot bound a pathological single key (one
	// container/day with unbounded samples). ~1M samples ≈ 200MB.
	defaultContainerStageSampleThreshold = 1 << 20
)

// stagedChunkKeys bounds the digest-key chunk held while finalizing staged
// samples — the per-key compute is streaming, so finalize memory is one
// chunk of keys plus its samples.
const stagedChunkKeys = 1024

// ContainerDigestBatch accumulates one manifest's container digests.
type ContainerDigestBatch struct {
	pool        *pgxpool.Pool
	orgID       string
	clusterUUID string
	manifestID  string

	groupedAll map[DigestKey][]metricSample
	groupedBH  map[DigestKey][]metricSample

	scheduleCache    *bhschedule.Cache
	gpuAccum         *gpuStreamAccumulator
	gpuBHAccum       *gpuStreamAccumulator
	nodeAccum        map[NodeDayKey]*NodeDayAccumulator
	nodeBHAccum      map[NodeDayKey]*NodeDayAccumulator
	nodeClusterSched bhschedule.Schedule
	writeNodeBH      bool
	writeGPUBH       bool

	totalRows           int
	memSamples          int
	stagedSamples       int
	digestBatchesStaged int
}

// NewContainerDigestBatch builds the manifest's shared stream state. Any
// staged rows left by a previous failed attempt are removed — the retry
// re-processes the un-done files from scratch (UNLOGGED staging truncates on
// crash anyway; this covers soft failures).
func NewContainerDigestBatch(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, manifestID string, opts ParseDigestOptions) (*ContainerDigestBatch, error) {
	// #758 review finding: Postgres normalizes UUID columns to lowercase,
	// so staged keys come back lowercase — normalize the in-memory keys the
	// same way or a remainder merge would miss its staged key (the later
	// memory-only upsert would then REPLACE the staged digest).
	clusterUUID = strings.ToLower(clusterUUID)
	b := &ContainerDigestBatch{
		pool:        pool,
		orgID:       orgID,
		clusterUUID: clusterUUID,
		manifestID:  manifestID,
		groupedAll:  make(map[DigestKey][]metricSample, defaultGroupedAllCapacity),
		groupedBH:   make(map[DigestKey][]metricSample, defaultGroupedBHCapacity),
	}
	if opts.EnableGPU {
		b.gpuAccum = newGPUStreamAccumulator()
	}
	if opts.EnableNode {
		b.nodeAccum = make(map[NodeDayKey]*NodeDayAccumulator, defaultNodeAccumCapacity)
	}
	if BusinessHoursAggregationEnabled() {
		var err error
		b.scheduleCache, err = bhschedule.LoadSchedules(ctx, pool, orgID, clusterUUID)
		if err != nil {
			return nil, fmt.Errorf("load business hours schedules: %w", err)
		}
		if b.scheduleCache != nil && !b.scheduleCache.ProducesBusinessHoursDigests() {
			if err := pruneBusinessHoursDigests(ctx, pool, orgID, clusterUUID); err != nil {
				return nil, err
			}
		}
		if b.scheduleCache != nil && !b.scheduleCache.ProducesNodeBusinessHoursDigests() {
			if err := bhschedule.PruneClusterNodeBusinessHoursDigests(ctx, pool, orgID, clusterUUID); err != nil {
				return nil, err
			}
		}
		if opts.EnableNode && b.scheduleCache != nil && b.scheduleCache.ProducesNodeBusinessHoursDigests() {
			b.nodeBHAccum = make(map[NodeDayKey]*NodeDayAccumulator, defaultNodeAccumCapacity)
			b.nodeClusterSched = b.scheduleCache.ResolveCluster()
			b.writeNodeBH = true
		}
		if opts.EnableGPU && b.scheduleCache != nil && b.scheduleCache.ProducesBusinessHoursDigests() {
			b.gpuBHAccum = newGPUStreamAccumulator()
			b.writeGPUBH = true
		}
	}
	if err := deleteStagedContainerSamples(ctx, pool, manifestID); err != nil {
		return nil, fmt.Errorf("clear staged samples for manifest %s: %w", manifestID, err)
	}
	return b, nil
}

// stageKeyThreshold returns the in-memory group bound: the flush knob when
// explicitly set (preserving its memory contract), else the batch default.
// The config layer defaults an unset knob to math.MaxInt32 — that sentinel
// is NOT a user choice and must not defeat the bound.
func (b *ContainerDigestBatch) stageKeyThreshold() int {
	if size := config.GetConfig().IngestFlushBatchSize; size > 0 && size != math.MaxInt32 {
		return size
	}
	return defaultContainerStageKeyThreshold
}

// AddFile streams one container CSV into the shared accumulators. Past a
// staging threshold, the in-memory groups stage to the per-manifest table
// (raw samples — lossless) and the maps clear, bounding memory. Returns the
// parsed rows for hook-dispatch contract parity (nil today: the container
// plugin's IngestCSV returns no rows).
func (b *ContainerDigestBatch) AddFile(ctx context.Context, r io.Reader) ([]MetricRow, error) {
	_, err := forEachCSVRow(ctx, r, func(row MetricRow) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		b.totalRows++
		b.memSamples++
		appendGroupedRow(b.groupedAll, row, b.orgID, b.clusterUUID, ScheduleTypeAllHours, nil)
		appendBusinessHoursRow(b.groupedBH, row, b.orgID, b.clusterUUID, b.scheduleCache)
		if b.gpuAccum != nil {
			b.gpuAccum.add(row)
		}
		if b.writeGPUBH {
			sched := b.scheduleCache.Resolve(row.Namespace)
			b.gpuBHAccum.addIfWeight(row, bhschedule.ScheduleWeight(row.IntervalStart, sched))
		}
		addNodeMetricRow(b.nodeAccum, row, 1)
		if b.writeNodeBH {
			addNodeMetricRow(b.nodeBHAccum, row, bhschedule.ScheduleWeight(row.IntervalStart, b.nodeClusterSched))
		}
		if b.memSamples >= defaultContainerStageSampleThreshold ||
			digestGroupCount(b.groupedAll, b.groupedBH) >= b.stageKeyThreshold() {
			if err := b.stageDigestGroups(ctx); err != nil {
				return fmt.Errorf("staging digest groups: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return nil, nil
}

// stageDigestGroups writes the in-memory sample groups to the staging table
// and clears the maps — the memory bound. Samples, not partial digests:
// staged data merges losslessly at finalize. A failure mid-way deletes the
// manifest's partial staged rows so the in-memory maps stay the single copy
// (no double count at finalize).
func (b *ContainerDigestBatch) stageDigestGroups(ctx context.Context) error {
	if len(b.groupedAll) == 0 && len(b.groupedBH) == 0 {
		return nil
	}
	start := time.Now()
	grouped := mergeDigestGroups(b.groupedAll, b.groupedBH)
	batch := &pgx.Batch{}
	queued := 0
	stageErr := func() error {
		for key, samples := range grouped {
			for _, s := range samples {
				batch.Queue(`
				INSERT INTO container_metric_samples_staging (
					manifest_id, org_id, cluster_uuid, namespace, workload, workload_type,
					container_name, bucket_date, schedule_type,
					interval_start, pod,
					cpu_request_mc, cpu_usage_mc, cpu_throttle_mc,
					mem_request_kib, mem_usage_kib, mem_rss_kib,
					oom_count, restart_count,
					workload_pod_count, desired_replicas, available_replicas
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`,
					b.manifestID, key.OrgID, key.ClusterUUID, key.Namespace, key.Workload, key.WorkloadType,
					key.ContainerName, key.BucketDate, string(key.ScheduleType),
					s.IntervalStart, s.Pod,
					s.CPURequestMC, s.CPUUsageMC, s.CPUThrottleMC,
					s.MemRequestKiB, s.MemUsageKiB, s.MemRSSKiB,
					s.OOMCount, s.RestartCount,
					s.WorkloadPodCount, s.DesiredReplicas, s.AvailableReplicas)
				queued++
				if queued >= 1000 {
					if err := flushQueuedBatch(ctx, b.pool, batch, queued); err != nil {
						return err
					}
					batch = &pgx.Batch{}
					queued = 0
				}
			}
		}
		return flushQueuedBatch(ctx, b.pool, batch, queued)
	}()
	if stageErr != nil {
		// Partial staging cleanup (#758 review finding): committed chunks
		// plus intact maps would double-count at finalize. Best effort — a
		// failing delete surfaces on the next batch construction anyway.
		if delErr := deleteStagedContainerSamples(ctx, b.pool, b.manifestID); delErr != nil {
			logging.ForOrg(b.orgID, b.clusterUUID).Errorf(
				"ContainerDigestBatch: failed to clean partial staging after stage error: %v", delErr)
		}
		return stageErr
	}
	b.stagedSamples += b.memSamples
	b.memSamples = 0
	clear(b.groupedAll)
	clear(b.groupedBH)
	b.digestBatchesStaged++
	metrics.SetIngestGroupsInMemory(0)
	logging.ForOrg(b.orgID, b.clusterUUID).Infof(
		"ContainerDigestBatch: staged %d samples across %d groups (batch %d)", queued, len(grouped), b.digestBatchesStaged)
	_ = start
	return nil
}

// Flush finalizes the manifest: digests are computed per key — staged rows
// stream in chunks with the in-memory remainder merged per key — and written
// with the same upsert paths the per-file stream uses. GPU/node accumulators
// (aggregated shapes, never staged) write here too, including hourly node
// digests. Staged rows are deleted on success; a failed Flush leaves files
// un-done, so the manifest retry re-processes from scratch.
func (b *ContainerDigestBatch) Flush(ctx context.Context) error {
	if b.totalRows == 0 && b.digestBatchesStaged == 0 {
		logging.ForOrg(b.orgID, b.clusterUUID).Info("ContainerDigestBatch: no rows parsed")
		return nil
	}

	singleTx := false
	if b.digestBatchesStaged > 0 {
		if err := b.finalizeStagedGroups(ctx); err != nil {
			return err
		}
	} else {
		grouped := mergeDigestGroups(b.groupedAll, b.groupedBH)
		metrics.SetIngestGroupsInMemory(len(grouped))
		if err := ensureDigestPartitionsForKeys(ctx, b.pool, grouped); err != nil {
			return fmt.Errorf("digest partitions: %w", err)
		}
		if singleIngestTxEligible(b.totalRows, len(grouped), 0) {
			// Partition prep for the GPU/node writers inside the single tx
			// (#758 review finding — mirrors pipeline_stream's EOF path).
			if b.gpuAccum != nil {
				months := map[time.Time]struct{}{}
				for k := range b.gpuAccum.groups {
					months[time.Date(k.date.Year(), k.date.Month(), 1, 0, 0, 0, 0, time.UTC)] = struct{}{}
				}
				if b.gpuBHAccum != nil {
					for k := range b.gpuBHAccum.groups {
						months[time.Date(k.date.Year(), k.date.Month(), 1, 0, 0, 0, 0, time.UTC)] = struct{}{}
					}
				}
				ensureGPUDigestPartitionsForMonths(ctx, b.pool, months)
			}
			if len(b.nodeAccum) > 0 {
				EnsureNodeDigestPartitions(ctx, b.pool, b.nodeAccum)
			}
			if len(b.nodeBHAccum) > 0 {
				EnsureNodeDigestPartitions(ctx, b.pool, b.nodeBHAccum)
			}
			if err := commitIngestInSingleTx(ctx, b.pool, grouped, b.gpuAccum, b.gpuBHAccum,
				b.nodeAccum, b.nodeBHAccum, b.scheduleCache, b.orgID, b.clusterUUID); err != nil {
				return err
			}
			singleTx = true
		} else if err := upsertContainerDigests(ctx, b.pool, grouped, b.scheduleCache); err != nil {
			return err
		}
	}

	if !singleTx {
		if b.gpuAccum != nil {
			if err := b.gpuAccum.flush(ctx, b.pool, b.orgID, b.clusterUUID); err != nil {
				return fmt.Errorf("GPU digest upsert: %w", err)
			}
		}
		if b.gpuBHAccum != nil {
			if err := b.gpuBHAccum.flushWithSchedule(ctx, b.pool, b.orgID, b.clusterUUID, ScheduleTypeBusinessHours); err != nil {
				return fmt.Errorf("GPU business_hours digest upsert: %w", err)
			}
		}
		if len(b.nodeAccum) > 0 {
			cfg := config.GetConfig()
			if err := FlushNodeDigests(ctx, b.pool, b.nodeAccum, b.orgID, b.clusterUUID, cfg.NodeAllocatableFactor); err != nil {
				return fmt.Errorf("node digest upsert: %w", err)
			}
		}
		if len(b.nodeBHAccum) > 0 {
			cfg := config.GetConfig()
			if err := FlushNodeDigestsWithSchedule(ctx, b.pool, b.nodeBHAccum, b.orgID, b.clusterUUID, cfg.NodeAllocatableFactor, ScheduleTypeBusinessHours); err != nil {
				return fmt.Errorf("node business_hours digest upsert: %w", err)
			}
		}
	}

	// Hourly node digests (#758 review finding — the per-file path writes
	// them at EOF; the batch must too or the node hourly-utilization API
	// loses data ingested via container files).
	if len(b.nodeAccum) > 0 && config.HourlyNodeDigestsEnabled() {
		hourlyMap := BuildHourlyNodeDigests(b.nodeAccum)
		if err := UpsertHourlyNodeDigests(ctx, b.pool, b.orgID, b.clusterUUID, hourlyMap); err != nil {
			return fmt.Errorf("hourly node digest upsert: %w", err)
		}
	}

	if b.digestBatchesStaged > 0 {
		if err := deleteStagedContainerSamples(ctx, b.pool, b.manifestID); err != nil {
			return fmt.Errorf("delete staged samples for manifest %s: %w", b.manifestID, err)
		}
	}
	logging.ForOrg(b.orgID, b.clusterUUID).Infof(
		"ContainerDigestBatch: finalized %d rows (staged batches: %d)", b.totalRows, b.digestBatchesStaged)
	return nil
}

// finalizeStagedGroups streams the staged samples per key (ORDER BY key),
// merging each key's in-memory remainder, and upserts digests in key chunks
// — memory stays bounded by one chunk plus its samples. Chunks flush only on
// KEY TRANSITIONS: a key's rows are contiguous under the ORDER BY, and
// flushing mid-key would split its samples across two replacement upserts
// (#758 review finding).
func (b *ContainerDigestBatch) finalizeStagedGroups(ctx context.Context) error {
	rows, err := b.pool.Query(ctx, `
		SELECT org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			bucket_date, schedule_type, interval_start, pod,
			cpu_request_mc, cpu_usage_mc, cpu_throttle_mc,
			mem_request_kib, mem_usage_kib, mem_rss_kib,
			oom_count, restart_count, workload_pod_count, desired_replicas, available_replicas
		FROM container_metric_samples_staging
		WHERE manifest_id = $1
		ORDER BY namespace, workload, workload_type, container_name, bucket_date, schedule_type`,
		b.manifestID)
	if err != nil {
		return fmt.Errorf("read staged samples: %w", err)
	}
	defer rows.Close()

	chunk := make(map[DigestKey][]metricSample, stagedChunkKeys)
	var currentKey DigestKey
	haveCurrent := false
	read := 0
	flushChunk := func() error {
		if len(chunk) == 0 {
			return nil
		}
		if err := ensureDigestPartitionsForKeys(ctx, b.pool, chunk); err != nil {
			return fmt.Errorf("digest partitions: %w", err)
		}
		if err := upsertContainerDigests(ctx, b.pool, chunk, b.scheduleCache); err != nil {
			return err
		}
		chunk = make(map[DigestKey][]metricSample, stagedChunkKeys)
		return nil
	}

	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var key DigestKey
		var scheduleType string
		var s metricSample
		if err := rows.Scan(&key.OrgID, &key.ClusterUUID, &key.Namespace, &key.Workload, &key.WorkloadType,
			&key.ContainerName, &key.BucketDate, &scheduleType, &s.IntervalStart, &s.Pod,
			&s.CPURequestMC, &s.CPUUsageMC, &s.CPUThrottleMC,
			&s.MemRequestKiB, &s.MemUsageKiB, &s.MemRSSKiB,
			&s.OOMCount, &s.RestartCount, &s.WorkloadPodCount, &s.DesiredReplicas, &s.AvailableReplicas); err != nil {
			return fmt.Errorf("scan staged sample: %w", err)
		}
		key.ScheduleType = ScheduleType(scheduleType)
		read++
		if haveCurrent && key != currentKey {
			// Key transition: safe to flush the chunk without splitting a
			// key's samples across replacement upserts.
			if len(chunk) >= stagedChunkKeys {
				if err := flushChunk(); err != nil {
					return err
				}
			}
		}
		currentKey, haveCurrent = key, true
		chunk[key] = append(chunk[key], s)
		// Merge this key's in-memory remainder (samples after the last
		// staging flush) exactly once.
		if rem, ok := b.groupedAll[key]; ok {
			chunk[key] = append(chunk[key], rem...)
			delete(b.groupedAll, key)
		}
		if rem, ok := b.groupedBH[key]; ok {
			chunk[key] = append(chunk[key], rem...)
			delete(b.groupedBH, key)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate staged samples: %w", err)
	}
	if err := flushChunk(); err != nil {
		return err
	}

	// Staged-row count verification (#758 review finding): an UNLOGGED
	// table truncates on a Postgres restart while the worker may survive —
	// finalizing against silently-missing staged rows would mark files done
	// with incomplete data. Any mismatch is a hard error (transient → the
	// manifest retry re-processes from scratch).
	if read != b.stagedSamples {
		return fmt.Errorf("staged sample count mismatch: wrote %d, read back %d (staging lost — retrying)",
			b.stagedSamples, read)
	}

	// Keys that never staged (memory-only remainder).
	grouped := mergeDigestGroups(b.groupedAll, b.groupedBH)
	if len(grouped) > 0 {
		if err := ensureDigestPartitionsForKeys(ctx, b.pool, grouped); err != nil {
			return fmt.Errorf("digest partitions: %w", err)
		}
		if err := upsertContainerDigests(ctx, b.pool, grouped, b.scheduleCache); err != nil {
			return err
		}
	}
	return nil
}

func deleteStagedContainerSamples(ctx context.Context, pool *pgxpool.Pool, manifestID string) error {
	if manifestID == "" {
		return nil
	}
	_, err := pool.Exec(ctx,
		`DELETE FROM container_metric_samples_staging WHERE manifest_id = $1`, manifestID)
	return err
}
