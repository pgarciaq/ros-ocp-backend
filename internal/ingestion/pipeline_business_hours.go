package ingestion

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgdigest"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

func buildBusinessHoursGroups(
	rows []MetricRow,
	orgID, clusterUUID string,
	cache *bhschedule.Cache,
) map[DigestKey][]metricSample {
	if cache == nil {
		return nil
	}
	out := make(map[DigestKey][]metricSample, len(rows)/24+1)
	for _, row := range rows {
		sched := cache.Resolve(row.Namespace)
		if !sched.Enabled {
			continue
		}
		weightFn := BusinessHoursRowWeightFn(sched)
		if weightFn != nil && weightFn(row) <= 0 {
			continue
		}
		bucketDate := time.Date(
			row.IntervalStart.Year(), row.IntervalStart.Month(), row.IntervalStart.Day(),
			0, 0, 0, 0, time.UTC,
		)
		key := DigestKey{
			OrgID:         orgID,
			ClusterUUID:   clusterUUID,
			Namespace:     row.Namespace,
			Workload:      row.WorkloadName,
			WorkloadType:  row.WorkloadType,
			ContainerName: row.ContainerName,
			BucketDate:    bucketDate,
			ScheduleType:  ScheduleTypeBusinessHours,
		}
		out[key] = append(out[key], metricSampleFromRow(row))
	}
	return out
}

func mergeDigestGroups(all, bh map[DigestKey][]metricSample) map[DigestKey][]metricSample {
	merged := make(map[DigestKey][]metricSample, len(all)+len(bh))
	for k, g := range all {
		merged[k] = g
	}
	for k, g := range bh {
		merged[k] = g
	}
	return merged
}

func rowWeightFnForDigestKey(key DigestKey, cache *bhschedule.Cache) SampleWeightFunc {
	if key.ScheduleType != ScheduleTypeBusinessHours || cache == nil {
		return nil
	}
	sched := cache.Resolve(key.Namespace)
	if !sched.Enabled {
		return nil
	}
	return BusinessHoursSampleWeightFn(sched)
}

// pruneBusinessHoursDigests removes business_hours digest rows when no enabled schedule applies
// (e.g. after DELETE of the last schedule row and re-ingestion).
func pruneBusinessHoursDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	return bhschedule.PruneClusterBusinessHoursDigests(ctx, pool, orgID, clusterUUID)
}

func upsertContainerDigests(
	ctx context.Context,
	pool *pgxpool.Pool,
	grouped map[DigestKey][]metricSample,
	scheduleCache *bhschedule.Cache,
) error {
	return withDeadlockRetry("upsert_container_digests", func() error {
		txDigests, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for container digests: %w", err)
		}
		defer txDigests.Rollback(ctx)
		if err := db.SetLocalIngestStatementTimeout(ctx, txDigests); err != nil {
			return fmt.Errorf("set ingest statement timeout: %w", err)
		}
		if err := upsertContainerDigestsOnSender(ctx, txDigests, grouped, scheduleCache); err != nil {
			return err
		}
		if err := txDigests.Commit(ctx); err != nil {
			return fmt.Errorf("commit container digests tx: %w", err)
		}
		return nil
	})
}

func upsertContainerDigestsOnSender(
	ctx context.Context,
	sender pgxBatchSender,
	grouped map[DigestKey][]metricSample,
	scheduleCache *bhschedule.Cache,
) error {
	digestKeys := make([]DigestKey, 0, len(grouped))
	for k := range grouped {
		digestKeys = append(digestKeys, k)
	}
	sortDigestKeys(digestKeys)

	rows := make([]pgdigest.Row, 0, len(digestKeys))
	for _, key := range digestKeys {
		group := grouped[key]
		weightFn := rowWeightFnForDigestKey(key, scheduleCache)
		d := ComputeContainerDigestWeighted(key, group, weightFn)
		rows = append(rows, pgdigest.Row{
			OrgID:        key.OrgID,
			ClusterUUID:  key.ClusterUUID,
			ScheduleType: string(key.ScheduleType),
			Digest:       keyedDigestFromComputed(d),
		})
	}
	return pgdigest.WriteRowsOnSender(ctx, sender, rows)
}

func keyedDigestFromComputed(d ContainerDigestResult) types.KeyedDigest {
	return types.KeyedDigest{
		Key: types.ContainerKey{
			Namespace:     d.Key.Namespace,
			Workload:      d.Key.Workload,
			WorkloadType:  d.Key.WorkloadType,
			ContainerName: d.Key.ContainerName,
		},
		Row: types.DigestRow{
			BucketDate:        d.Key.BucketDate,
			CPURequestP50MC:   d.CPURequestP50MC,
			CPURequestP60MC:   d.CPURequestP60MC,
			CPURequestP95MC:   d.CPURequestP95MC,
			CPURequestP98MC:   d.CPURequestP98MC,
			CPURequestP99MC:   d.CPURequestP99MC,
			CPUUsageP50MC:     d.CPUUsageP50MC,
			CPUUsageP60MC:     d.CPUUsageP60MC,
			CPUUsageP95MC:     d.CPUUsageP95MC,
			CPUUsageP98MC:     d.CPUUsageP98MC,
			CPUUsageP99MC:     d.CPUUsageP99MC,
			CPUUsageMaxMC:     d.CPUUsageMaxMC,
			CPUThrottleP95MC:  d.CPUThrottleP95MC,
			CPUThrottleMaxMC:  d.CPUThrottleMaxMC,
			MemRequestP50KiB:  d.MemRequestP50KiB,
			MemRequestP60KiB:  d.MemRequestP60KiB,
			MemRequestP95KiB:  d.MemRequestP95KiB,
			MemRequestP98KiB:  d.MemRequestP98KiB,
			MemRequestP99KiB:  d.MemRequestP99KiB,
			MemUsageP50KiB:    d.MemUsageP50KiB,
			MemUsageP60KiB:    d.MemUsageP60KiB,
			MemUsageP95KiB:    d.MemUsageP95KiB,
			MemUsageP98KiB:    d.MemUsageP98KiB,
			MemUsageP99KiB:    d.MemUsageP99KiB,
			MemUsageMaxKiB:    d.MemUsageMaxKiB,
			MemRSSP95KiB:      d.MemRSSP95KiB,
			MemRSSMaxKiB:      d.MemRSSMaxKiB,
			OOMCountSum:       d.OOMCountSum,
			CPUUsageMeanMC:    d.CPUUsageMeanMC,
			MemUsageMeanKiB:   d.MemUsageMeanKiB,
			SampleCount:       d.SampleCount,
			PodCountMin:       d.PodCountMin,
			PodCountMax:       d.PodCountMax,
			PodCountAvg:       d.PodCountAvg,
			DesiredReplicas:   d.DesiredReplicas,
			AvailableReplicas: d.AvailableReplicas,
			CPUUsageCVBP:      d.CPUUsageCVBP,
		},
	}
}
