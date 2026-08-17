package ingestion

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/fixedpoint"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

type gpuStreamKey struct {
	date      time.Time
	namespace string
	workload  string
	container string
}

type gpuStreamAgg struct {
	workloadType string
	modelName    string
	profileName  string
	nodeName     string
	count        int
	fbMinVal     int32
	fbMaxVal     int32
	fbAvgSum     int64
	tensorMinVal int32
	tensorMaxVal int32
	tensorAvgSum int64
	dramMinVal   int32
	dramMaxVal   int32
	dramAvgSum   int64
	smMinVal     int32
	smMaxVal     int32
	smAvgSum     int64
	gpuUUIDs     map[string]struct{}
}

type gpuStreamAccumulator struct {
	groups map[gpuStreamKey]*gpuStreamAgg
}

func newGPUStreamAccumulator() *gpuStreamAccumulator {
	return &gpuStreamAccumulator{groups: make(map[gpuStreamKey]*gpuStreamAgg)}
}

// addIfWeight includes the sample when w > 0 (full sample; no fractional min/max).
func (a *gpuStreamAccumulator) addIfWeight(r MetricRow, w float64) {
	if w <= 0 {
		return
	}
	a.add(r)
}

func (a *gpuStreamAccumulator) add(r MetricRow) {
	if a == nil || !r.HasGPU() {
		return
	}
	day := time.Date(r.IntervalStart.Year(), r.IntervalStart.Month(), r.IntervalStart.Day(), 0, 0, 0, 0, time.UTC)
	k := gpuStreamKey{date: day, namespace: r.Namespace, workload: r.WorkloadName, container: r.ContainerName}
	fbMin := int32(math.Round(r.AcceleratorFBUsageMin))
	fbMax := int32(math.Round(r.AcceleratorFBUsageMax))
	fbAvg := int32(math.Round(r.AcceleratorFBUsageAvg))
	tensorMin := fixedpoint.FloatToBasisPoints(r.TensorPipeActiveMin)
	tensorMax := fixedpoint.FloatToBasisPoints(r.TensorPipeActiveMax)
	tensorAvg := fixedpoint.FloatToBasisPoints(r.TensorPipeActiveAvg)
	dramMin := fixedpoint.FloatToBasisPoints(r.DRAMActiveMin)
	dramMax := fixedpoint.FloatToBasisPoints(r.DRAMActiveMax)
	dramAvg := fixedpoint.FloatToBasisPoints(r.DRAMActiveAvg)
	smMin := fixedpoint.FloatToBasisPoints(r.SMActiveMin)
	smMax := fixedpoint.FloatToBasisPoints(r.SMActiveMax)
	smAvg := fixedpoint.FloatToBasisPoints(r.SMActiveAvg)

	g, ok := a.groups[k]
	if !ok {
		g = &gpuStreamAgg{
			workloadType: r.WorkloadType,
			modelName:    r.AcceleratorModelName,
			profileName:  r.AcceleratorProfileName,
			fbMinVal:     fbMin,
			fbMaxVal:     fbMax,
			tensorMinVal: tensorMin,
			tensorMaxVal: tensorMax,
			dramMinVal:   dramMin,
			dramMaxVal:   dramMax,
			smMinVal:     smMin,
			smMaxVal:     smMax,
			gpuUUIDs:     make(map[string]struct{}),
		}
		a.groups[k] = g
	} else {
		if fbMin < g.fbMinVal {
			g.fbMinVal = fbMin
		}
		if fbMax > g.fbMaxVal {
			g.fbMaxVal = fbMax
		}
		if tensorMin < g.tensorMinVal {
			g.tensorMinVal = tensorMin
		}
		if tensorMax > g.tensorMaxVal {
			g.tensorMaxVal = tensorMax
		}
		if dramMin < g.dramMinVal {
			g.dramMinVal = dramMin
		}
		if dramMax > g.dramMaxVal {
			g.dramMaxVal = dramMax
		}
		if smMin < g.smMinVal {
			g.smMinVal = smMin
		}
		if smMax > g.smMaxVal {
			g.smMaxVal = smMax
		}
	}
	if r.Node != "" {
		g.nodeName = r.Node
	}
	if r.GPUUUID != "" {
		g.gpuUUIDs[r.GPUUUID] = struct{}{}
	}
	g.count++
	g.fbAvgSum += int64(fbAvg)
	g.tensorAvgSum += int64(tensorAvg)
	g.dramAvgSum += int64(dramAvg)
	g.smAvgSum += int64(smAvg)
}

func (a *gpuStreamAccumulator) flush(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	return a.flushWithSchedule(ctx, pool, orgID, clusterUUID, ScheduleTypeAllHours)
}

func (a *gpuStreamAccumulator) flushWithSchedule(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, scheduleType ScheduleType) error {
	if a == nil || len(a.groups) == 0 {
		return nil
	}
	if scheduleType == "" {
		scheduleType = ScheduleTypeAllHours
	}
	months := map[time.Time]struct{}{}
	for k := range a.groups {
		monthStart := time.Date(k.date.Year(), k.date.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[monthStart] = struct{}{}
	}
	ensureGPUDigestPartitionsForMonths(ctx, pool, months)
	return flushGPUStreamGroups(ctx, pool, a.groups, clusterUUID, orgID, scheduleType)
}

func ensureGPUDigestPartitionsForMonths(ctx context.Context, pool *pgxpool.Pool, months map[time.Time]struct{}) {
	for monthStart := range months {
		monthEnd := monthStart.AddDate(0, 1, 0)
		partName := fmt.Sprintf("gpu_container_digests_%s", monthStart.Format("200601"))
		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF gpu_container_digests FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			monthStart.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, sql); err != nil {
			logging.GetLogger().Warnf("EnsureGPUDigestPartitions: %s: %v (non-fatal)", partName, err)
		}
	}
}

func flushGPUStreamGroups(ctx context.Context, pool *pgxpool.Pool, groups map[gpuStreamKey]*gpuStreamAgg, clusterUUID, orgID string, scheduleType ScheduleType) error {
	err := withDeadlockRetry("flush_gpu_stream_groups", func() error {
		txGPU, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for GPU digests: %w", err)
		}
		defer txGPU.Rollback(ctx)
		if err := db.SetLocalIngestStatementTimeout(ctx, txGPU); err != nil {
			return fmt.Errorf("set ingest statement timeout: %w", err)
		}
		if err := flushGPUStreamGroupsOnSender(ctx, txGPU, groups, clusterUUID, scheduleType); err != nil {
			return err
		}
		if err := txGPU.Commit(ctx); err != nil {
			return fmt.Errorf("commit GPU digests tx: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	logging.ForOrg(orgID, clusterUUID).Infof("ProcessCSVToDigests: upserted %d GPU digests schedule_type=%s", len(groups), scheduleType)
	return nil
}

func flushGPUStreamGroupsOnSender(ctx context.Context, sender pgxBatchSender, groups map[gpuStreamKey]*gpuStreamAgg, clusterUUID string, scheduleType ScheduleType) error {
	if len(groups) == 0 {
		return nil
	}
	if scheduleType == "" {
		scheduleType = ScheduleTypeAllHours
	}
	type gpuGroupEntry struct {
		key gpuStreamKey
		agg *gpuStreamAgg
	}
	gpuEntries := make([]gpuGroupEntry, 0, len(groups))
	for k, g := range groups {
		gpuEntries = append(gpuEntries, gpuGroupEntry{key: k, agg: g})
	}
	slices.SortFunc(gpuEntries, func(a, b gpuGroupEntry) int {
		if a.key.date.Before(b.key.date) {
			return -1
		}
		if a.key.date.After(b.key.date) {
			return 1
		}
		if c := cmpStr(a.key.namespace, b.key.namespace); c != 0 {
			return c
		}
		if c := cmpStr(a.key.workload, b.key.workload); c != 0 {
			return c
		}
		return cmpStr(a.key.container, b.key.container)
	})
	for chunkStart := 0; chunkStart < len(gpuEntries); chunkStart += db.MaxPgxBatchQueue {
		chunkEnd := chunkStart + db.MaxPgxBatchQueue
		if chunkEnd > len(gpuEntries) {
			chunkEnd = len(gpuEntries)
		}
		batch := &pgx.Batch{}
		for _, entry := range gpuEntries[chunkStart:chunkEnd] {
			k, g := entry.key, entry.agg
			gpuCount := len(g.gpuUUIDs)
			if gpuCount < 1 {
				gpuCount = 1
			}
			batch.Queue(`
			INSERT INTO gpu_container_digests (
				interval_start, cluster_uuid, namespace, workload, workload_type, container_name,
				gpu_model_name, gpu_profile_name, node_name,
				fb_usage_min_mib, fb_usage_max_mib, fb_usage_avg_mib,
				tensor_pipe_active_min, tensor_pipe_active_max, tensor_pipe_active_avg,
				dram_active_min, dram_active_max, dram_active_avg,
				sm_active_min, sm_active_max, sm_active_avg,
				gpu_count, schedule_type
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
			ON CONFLICT (cluster_uuid, namespace, workload, container_name, gpu_model_name, interval_start, schedule_type)
			DO UPDATE SET
				gpu_profile_name = EXCLUDED.gpu_profile_name,
				node_name = EXCLUDED.node_name,
				fb_usage_min_mib = EXCLUDED.fb_usage_min_mib,
				fb_usage_max_mib = EXCLUDED.fb_usage_max_mib,
				fb_usage_avg_mib = EXCLUDED.fb_usage_avg_mib,
				tensor_pipe_active_min = EXCLUDED.tensor_pipe_active_min,
				tensor_pipe_active_max = EXCLUDED.tensor_pipe_active_max,
				tensor_pipe_active_avg = EXCLUDED.tensor_pipe_active_avg,
				dram_active_min = EXCLUDED.dram_active_min,
				dram_active_max = EXCLUDED.dram_active_max,
				dram_active_avg = EXCLUDED.dram_active_avg,
				sm_active_min = EXCLUDED.sm_active_min,
				sm_active_max = EXCLUDED.sm_active_max,
				sm_active_avg = EXCLUDED.sm_active_avg,
				gpu_count = EXCLUDED.gpu_count`,
				k.date, clusterUUID, k.namespace, k.workload, g.workloadType, k.container,
				g.modelName, g.profileName, g.nodeName,
				g.fbMinVal, g.fbMaxVal, safeMeanInt32(g.fbAvgSum, g.count),
				g.tensorMinVal, g.tensorMaxVal, safeMeanInt32(g.tensorAvgSum, g.count),
				g.dramMinVal, g.dramMaxVal, safeMeanInt32(g.dramAvgSum, g.count),
				g.smMinVal, g.smMaxVal, safeMeanInt32(g.smAvgSum, g.count),
				gpuCount, scheduleType,
			)
		}
		if err := flushQueuedBatch(ctx, sender, batch, chunkEnd-chunkStart); err != nil {
			return fmt.Errorf("upsert GPU digest: %w", err)
		}
	}
	return nil
}
