package ingestion

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// NodeDayKey uniquely identifies a node-day combination for aggregation.
type NodeDayKey struct {
	OrgID       string
	ClusterUUID string
	Node        string
	BucketDate  time.Time
}

const nodeDayHours = 24

// NodeDayAccumulator collects per-hour samples for a single node-day,
// allowing computation of usage percentiles and request/capacity maximums.
type NodeDayAccumulator struct {
	intervalCPUReqs   [nodeDayHours]int64
	intervalMemReqs   [nodeDayHours]int64
	intervalCPUUse    [nodeDayHours]int64
	intervalMemUse    [nodeDayHours]int64
	intervalPods      [nodeDayHours]int64
	intervalSeen      [nodeDayHours]bool
	MaxCPUCapacityMC  int64
	MaxMemCapacityKiB int64
}

func newNodeDayAccumulator() *NodeDayAccumulator {
	return &NodeDayAccumulator{}
}

func hourIndex(t time.Time) int {
	return t.UTC().Hour()
}

// AddRow accumulates a single container metric row into this node-day.
func (a *NodeDayAccumulator) AddRow(r MetricRow) {
	h := hourIndex(r.IntervalStart)
	if !a.intervalSeen[h] {
		a.intervalSeen[h] = true
	}
	a.intervalCPUReqs[h] += r.CPURequestMC
	a.intervalMemReqs[h] += r.MemRequestKiB
	a.intervalCPUUse[h] += r.CPUUsageMC
	a.intervalMemUse[h] += r.MemUsageKiB
	a.intervalPods[h]++
	if r.NodeCapacityCPUMC > 0 && r.NodeCapacityCPUMC > a.MaxCPUCapacityMC {
		a.MaxCPUCapacityMC = r.NodeCapacityCPUMC
	}
	if r.NodeCapacityMemKiB > 0 && r.NodeCapacityMemKiB > a.MaxMemCapacityKiB {
		a.MaxMemCapacityKiB = r.NodeCapacityMemKiB
	}
}

// Finalize computes the summary statistics from accumulated interval data.
func (a *NodeDayAccumulator) Finalize() (cpuP50, cpuP95, memP50, memP95, maxCPUReq, maxMemReq int64, maxPods int64, sampleCount int64) {
	cpuUsageSamples := make([]int64, 0, nodeDayHours)
	memUsageSamples := make([]int64, 0, nodeDayHours)
	for h := 0; h < nodeDayHours; h++ {
		if !a.intervalSeen[h] {
			continue
		}
		cpuUsageSamples = append(cpuUsageSamples, a.intervalCPUUse[h])
		memUsageSamples = append(memUsageSamples, a.intervalMemUse[h])
		if a.intervalCPUReqs[h] > maxCPUReq {
			maxCPUReq = a.intervalCPUReqs[h]
		}
		if a.intervalMemReqs[h] > maxMemReq {
			maxMemReq = a.intervalMemReqs[h]
		}
		if a.intervalPods[h] > maxPods {
			maxPods = a.intervalPods[h]
		}
	}

	sampleCount = int64(len(cpuUsageSamples))
	if sampleCount == 0 {
		return
	}

	slices.Sort(cpuUsageSamples)
	slices.Sort(memUsageSamples)

	cpuP50 = percentileInt64(cpuUsageSamples, 0.50)
	cpuP95 = percentileInt64(cpuUsageSamples, 0.95)
	memP50 = percentileInt64(memUsageSamples, 0.50)
	memP95 = percentileInt64(memUsageSamples, 0.95)
	return
}

func percentileInt64(sorted []int64, pct float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(pct * float64(len(sorted)-1))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// AggregateNodeDigests groups MetricRows by (node, day) and returns the
// accumulated data ready for flushing to the database.
func AggregateNodeDigests(rows []MetricRow) map[NodeDayKey]*NodeDayAccumulator {
	accumulators := make(map[NodeDayKey]*NodeDayAccumulator)
	for _, r := range rows {
		if r.Node == "" {
			continue
		}
		day := time.Date(r.IntervalStart.Year(), r.IntervalStart.Month(), r.IntervalStart.Day(), 0, 0, 0, 0, time.UTC)
		key := NodeDayKey{Node: r.Node, BucketDate: day}
		acc, ok := accumulators[key]
		if !ok {
			acc = newNodeDayAccumulator()
			accumulators[key] = acc
		}
		acc.AddRow(r)
	}
	return accumulators
}

// EnsureNodeDigestPartitions creates monthly partitions of daily_node_digests.
func EnsureNodeDigestPartitions(ctx context.Context, pool *pgxpool.Pool, keys map[NodeDayKey]*NodeDayAccumulator) {
	months := map[time.Time]struct{}{}
	for k := range keys {
		monthStart := time.Date(k.BucketDate.Year(), k.BucketDate.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[monthStart] = struct{}{}
	}
	ensureNodeDigestPartitionsForMonths(ctx, pool, months)
}

func ensureNodeDigestPartitionsForMonths(ctx context.Context, pool *pgxpool.Pool, months map[time.Time]struct{}) {
	for monthStart := range months {
		monthEnd := monthStart.AddDate(0, 1, 0)
		partName := fmt.Sprintf("daily_node_digests_%s", monthStart.Format("200601"))
		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_node_digests FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			monthStart.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, sql); err != nil {
			logging.GetLogger().Warnf("EnsureNodeDigestPartitions: %s: %v (non-fatal)", partName, err)
		}
	}
}

type nodeDigestEntry struct {
	key NodeDayKey
	acc *NodeDayAccumulator
}

// FlushNodeDigests computes final statistics and upserts node digests to the database.
func FlushNodeDigests(ctx context.Context, pool *pgxpool.Pool, accumulators map[NodeDayKey]*NodeDayAccumulator, orgID, clusterUUID string, allocatableFactor float64) error {
	if len(accumulators) == 0 {
		return nil
	}

	EnsureNodeDigestPartitions(ctx, pool, accumulators)

	entries := make([]nodeDigestEntry, 0, len(accumulators))
	for k, acc := range accumulators {
		entries = append(entries, nodeDigestEntry{key: k, acc: acc})
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for node digests: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := flushNodeDigestsOnSender(ctx, tx, entries, orgID, clusterUUID, allocatableFactor); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit node digests tx: %w", err)
	}

	logging.ForOrg(orgID, clusterUUID).Infof("FlushNodeDigests: upserted %d node digests",
		len(accumulators))
	return nil
}

func flushNodeDigestsOnSender(
	ctx context.Context,
	sender pgxBatchSender,
	entries []nodeDigestEntry,
	orgID, clusterUUID string,
	allocatableFactor float64,
) error {
	for chunkStart := 0; chunkStart < len(entries); chunkStart += maxPgxBatchQueue {
		chunkEnd := chunkStart + maxPgxBatchQueue
		if chunkEnd > len(entries) {
			chunkEnd = len(entries)
		}
		batch := &pgx.Batch{}
		for _, ent := range entries[chunkStart:chunkEnd] {
			key, acc := ent.key, ent.acc
			cpuP50, cpuP95, memP50, memP95, maxCPUReq, maxMemReq, maxPods, sampleCount := acc.Finalize()

			var allocCPU, allocMem *int64
			if acc.MaxCPUCapacityMC > 0 {
				v := int64(float64(acc.MaxCPUCapacityMC) * allocatableFactor)
				allocCPU = &v
			}
			if acc.MaxMemCapacityKiB > 0 {
				v := int64(float64(acc.MaxMemCapacityKiB) * allocatableFactor)
				allocMem = &v
			}

			batch.Queue(`
			INSERT INTO daily_node_digests (
				bucket_date, org_id, cluster_uuid, node,
				cpu_usage_p50_mc, cpu_usage_p95_mc,
				mem_usage_p50_kib, mem_usage_p95_kib,
				max_cpu_allocatable_mc, max_mem_allocatable_kib,
				max_cpu_requests_mc, max_mem_requests_kib,
				max_pod_count, sample_count
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			ON CONFLICT (org_id, cluster_uuid, node, bucket_date)
			DO UPDATE SET
				cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc,
				cpu_usage_p95_mc = EXCLUDED.cpu_usage_p95_mc,
				mem_usage_p50_kib = EXCLUDED.mem_usage_p50_kib,
				mem_usage_p95_kib = EXCLUDED.mem_usage_p95_kib,
				max_cpu_allocatable_mc = EXCLUDED.max_cpu_allocatable_mc,
				max_mem_allocatable_kib = EXCLUDED.max_mem_allocatable_kib,
				max_cpu_requests_mc = EXCLUDED.max_cpu_requests_mc,
				max_mem_requests_kib = EXCLUDED.max_mem_requests_kib,
				max_pod_count = EXCLUDED.max_pod_count,
				sample_count = EXCLUDED.sample_count`,
				key.BucketDate.Format("2006-01-02"), orgID, clusterUUID, key.Node,
				cpuP50, cpuP95, memP50, memP95,
				allocCPU, allocMem,
				maxCPUReq, maxMemReq, maxPods, sampleCount,
			)
		}
		if err := flushQueuedBatch(ctx, sender, batch, chunkEnd-chunkStart); err != nil {
			return fmt.Errorf("upsert node digest: %w", err)
		}
	}
	return nil
}
