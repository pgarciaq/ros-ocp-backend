package ingestion

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// NodeHourlyDigestKey identifies a single node-hour digest group.
type NodeHourlyDigestKey struct {
	NodeName   string
	BucketDate time.Time
	Hour       int
}

// NodeHourlyDigestResult is an hourly aggregated node digest ready for database upsert.
type NodeHourlyDigestResult struct {
	NodeName       string
	BucketDate     time.Time
	Hour           int
	CPUUsageP95MC  int64
	MemUsageP95KiB int64
	SampleCount    int32
	MaxPodCount    int32
}

// BuildHourlyNodeDigests extracts hourly-level data from the already-aggregated
// NodeDayAccumulator map. Each accumulator stores per-hour totals via hourIndex(),
// so this function simply reads those per-hour buckets and produces one row per
// (node, date, hour) that had at least one sample.
func BuildHourlyNodeDigests(accumulators map[NodeDayKey]*NodeDayAccumulator) map[NodeHourlyDigestKey]NodeHourlyDigestResult {
	out := make(map[NodeHourlyDigestKey]NodeHourlyDigestResult)

	for key, acc := range accumulators {
		for h := 0; h < nodeDayHours; h++ {
			if !acc.intervalSeen[h] {
				continue
			}

			hKey := NodeHourlyDigestKey{
				NodeName:   key.Node,
				BucketDate: key.BucketDate,
				Hour:       h,
			}

			podCount := int32(0)
			if acc.intervalPodsDistinct[h] != nil {
				podCount = int32(len(acc.intervalPodsDistinct[h]))
			}

			out[hKey] = NodeHourlyDigestResult{
				NodeName:       key.Node,
				BucketDate:     key.BucketDate,
				Hour:           h,
				CPUUsageP95MC:  acc.intervalCPUUse[h],
				MemUsageP95KiB: acc.intervalMemUse[h],
				SampleCount:    1,
				MaxPodCount:    podCount,
			}
		}
	}
	return out
}

// EnsureHourlyNodeDigestPartitions creates monthly partitions for hourly_node_digests.
func EnsureHourlyNodeDigestPartitions(ctx context.Context, pool *pgxpool.Pool, digests map[NodeHourlyDigestKey]NodeHourlyDigestResult) {
	months := map[time.Time]struct{}{}
	for k := range digests {
		monthStart := time.Date(k.BucketDate.Year(), k.BucketDate.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[monthStart] = struct{}{}
	}
	for monthStart := range months {
		monthEnd := monthStart.AddDate(0, 1, 0)
		partName := fmt.Sprintf("hourly_node_digests_%s", monthStart.Format("200601"))
		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF hourly_node_digests FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			monthStart.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, sql); err != nil {
			logging.GetLogger().Warnf("EnsureHourlyNodeDigestPartitions: %s: %v (non-fatal)", partName, err)
		}
	}
}

// UpsertHourlyNodeDigests writes hourly node digests using pgx.Batch.
func UpsertHourlyNodeDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, digests map[NodeHourlyDigestKey]NodeHourlyDigestResult) error {
	if len(digests) == 0 {
		return nil
	}

	EnsureHourlyNodeDigestPartitions(ctx, pool, digests)

	batch := &pgx.Batch{}
	for _, d := range digests {
		batch.Queue(`
			INSERT INTO hourly_node_digests (
				org_id, cluster_uuid, node_name, report_date, hour,
				cpu_usage_p95_mc, mem_usage_p95_kib, sample_count,
				max_pod_count, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())
			ON CONFLICT (org_id, cluster_uuid, node_name, report_date, hour)
			DO UPDATE SET
				cpu_usage_p95_mc = EXCLUDED.cpu_usage_p95_mc,
				mem_usage_p95_kib = EXCLUDED.mem_usage_p95_kib,
				sample_count = EXCLUDED.sample_count,
				max_pod_count = EXCLUDED.max_pod_count,
				updated_at = NOW()`,
			orgID, clusterUUID, d.NodeName,
			d.BucketDate.Format("2006-01-02"), d.Hour,
			d.CPUUsageP95MC, d.MemUsageP95KiB, d.SampleCount,
			d.MaxPodCount,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()
	for range digests {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("upsert hourly node digest: %w", err)
		}
	}
	return nil
}
