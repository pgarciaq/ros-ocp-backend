package ingestion

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

var knownVMPartitions sync.Map

// VMHourlyDigestKey identifies a single VM-hour digest group.
type VMHourlyDigestKey struct {
	VMName     string
	Namespace  string
	BucketDate time.Time
	Hour       int
}

// VMHourlyDigestResult is an hourly aggregated VM digest ready for database upsert.
type VMHourlyDigestResult struct {
	VMName           string
	Namespace        string
	BucketDate       time.Time
	Hour             int
	CPUUsageP95MC    int64
	MemUsageP95KiB   int64
	SampleCount      int32
	DiskReadIOPSP95  int64
	DiskWriteIOPSP95 int64
}

type vmHourlyAccumulator struct {
	cpuUsage      []float64
	memUsage      []float64
	diskReadIOPS  []float64
	diskWriteIOPS []float64
	sampleCount   int
}

func newVMHourlyAccumulator() *vmHourlyAccumulator {
	return &vmHourlyAccumulator{
		cpuUsage:      make([]float64, 0, 4),
		memUsage:      make([]float64, 0, 4),
		diskReadIOPS:  make([]float64, 0, 4),
		diskWriteIOPS: make([]float64, 0, 4),
	}
}

// BuildHourlyVMDigests aggregates 15-minute VM samples into hourly digests
// keyed by (vm_name, namespace, bucket_date, hour).
func BuildHourlyVMDigests(rows []VMRow) map[VMHourlyDigestKey]VMHourlyDigestResult {
	groups := make(map[VMHourlyDigestKey]*vmHourlyAccumulator)
	for _, r := range rows {
		addVMRowToHourlyGroups(groups, r)
	}
	return finalizeHourlyVMGroups(groups)
}

func addVMRowToHourlyGroups(groups map[VMHourlyDigestKey]*vmHourlyAccumulator, r VMRow) {
	bucketDate := vmBucketDate(r.IntervalStart)
	hour := r.IntervalStart.UTC().Hour()
	key := VMHourlyDigestKey{
		VMName:     r.VMName,
		Namespace:  r.Namespace,
		BucketDate: bucketDate,
		Hour:       hour,
	}

	acc, ok := groups[key]
	if !ok {
		acc = newVMHourlyAccumulator()
		groups[key] = acc
	}

	acc.cpuUsage = append(acc.cpuUsage, r.CPUUsageMC)
	acc.memUsage = append(acc.memUsage, r.MemoryUsageKiB)

	if r.DiskReadIOPS != nil {
		acc.diskReadIOPS = append(acc.diskReadIOPS, *r.DiskReadIOPS)
	}
	if r.DiskWriteIOPS != nil {
		acc.diskWriteIOPS = append(acc.diskWriteIOPS, *r.DiskWriteIOPS)
	}

	acc.sampleCount++
}

func finalizeHourlyVMGroups(groups map[VMHourlyDigestKey]*vmHourlyAccumulator) map[VMHourlyDigestKey]VMHourlyDigestResult {
	out := make(map[VMHourlyDigestKey]VMHourlyDigestResult, len(groups))
	for key, acc := range groups {
		d := VMHourlyDigestResult{
			VMName:      key.VMName,
			Namespace:   key.Namespace,
			BucketDate:  key.BucketDate,
			Hour:        key.Hour,
			SampleCount: int32(acc.sampleCount),
		}

		sortedCPU := sortedCopy(acc.cpuUsage)
		d.CPUUsageP95MC = roundFloat64ToInt64(percentileFloat(sortedCPU, 0.95))

		sortedMem := sortedCopy(acc.memUsage)
		d.MemUsageP95KiB = roundFloat64ToInt64(percentileFloat(sortedMem, 0.95))

		if len(acc.diskReadIOPS) > 0 {
			d.DiskReadIOPSP95 = roundFloat64ToInt64(percentileFloat(sortedCopy(acc.diskReadIOPS), 0.95))
		}
		if len(acc.diskWriteIOPS) > 0 {
			d.DiskWriteIOPSP95 = roundFloat64ToInt64(percentileFloat(sortedCopy(acc.diskWriteIOPS), 0.95))
		}

		out[key] = d
	}
	return out
}

// EnsureHourlyVMDigestPartitions creates monthly partitions for hourly_vm_digests.
func EnsureHourlyVMDigestPartitions(ctx context.Context, pool *pgxpool.Pool, digests map[VMHourlyDigestKey]VMHourlyDigestResult) {
	months := map[time.Time]struct{}{}
	for k := range digests {
		monthStart := time.Date(k.BucketDate.Year(), k.BucketDate.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[monthStart] = struct{}{}
	}
	for monthStart := range months {
		partName := fmt.Sprintf("hourly_vm_digests_%s", monthStart.Format("200601"))
		if _, loaded := knownVMPartitions.LoadOrStore(partName, struct{}{}); loaded {
			continue
		}
		monthEnd := monthStart.AddDate(0, 1, 0)
		ddl := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF hourly_vm_digests FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			monthStart.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, ddl); err != nil {
			knownVMPartitions.Delete(partName)
			logging.GetLogger().Warnf("EnsureHourlyVMDigestPartitions: %s: %v (non-fatal)", partName, err)
			continue
		}
		relopts := fmt.Sprintf(
			`ALTER TABLE %s SET (autovacuum_vacuum_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.02, fillfactor = 85)`,
			partName,
		)
		if _, err := pool.Exec(ctx, relopts); err != nil {
			logging.GetLogger().Warnf("EnsureHourlyVMDigestPartitions: reloptions %s: %v (non-fatal)", partName, err)
		}
	}
}

// UpsertHourlyVMDigests writes hourly VM digests using pgx.Batch.
func UpsertHourlyVMDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, digests map[VMHourlyDigestKey]VMHourlyDigestResult) error {
	if len(digests) == 0 {
		return nil
	}

	EnsureHourlyVMDigestPartitions(ctx, pool, digests)

	batch := &pgx.Batch{}
	for _, d := range digests {
		batch.Queue(`
			INSERT INTO hourly_vm_digests (
				org_id, cluster_uuid, namespace, vm_name, report_date, hour,
				cpu_usage_p95_mc, mem_usage_p95_kib, sample_count,
				disk_read_iops_p95, disk_write_iops_p95, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
			ON CONFLICT (org_id, cluster_uuid, namespace, vm_name, report_date, hour)
			DO UPDATE SET
				cpu_usage_p95_mc = EXCLUDED.cpu_usage_p95_mc,
				mem_usage_p95_kib = EXCLUDED.mem_usage_p95_kib,
				sample_count = EXCLUDED.sample_count,
				disk_read_iops_p95 = EXCLUDED.disk_read_iops_p95,
				disk_write_iops_p95 = EXCLUDED.disk_write_iops_p95,
				updated_at = NOW()`,
			orgID, clusterUUID, d.Namespace, d.VMName,
			d.BucketDate.Format("2006-01-02"), d.Hour,
			d.CPUUsageP95MC, d.MemUsageP95KiB, d.SampleCount,
			d.DiskReadIOPSP95, d.DiskWriteIOPSP95,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()
	for range digests {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("upsert hourly VM digest: %w", err)
		}
	}
	return nil
}
