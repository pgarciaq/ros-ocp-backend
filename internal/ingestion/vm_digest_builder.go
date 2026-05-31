package ingestion

import (
	"context"
	"fmt"
	"io"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// VMDigestResult is a daily aggregated VM digest ready for database upsert.
type VMDigestResult struct {
	VMName     string
	Namespace  string
	NodeName   string
	GuestOS    string
	BucketDate time.Time

	CPUUsageP50MC int64
	CPUUsageP95MC int64
	CPUUsageP99MC int64
	CPUUsageMaxMC int64
	CPURequestMC  int64
	CPULimitMC    int64

	MemUsageP50KiB int64
	MemUsageP95KiB int64
	MemUsageP99KiB int64
	MemUsageMaxKiB int64
	MemRequestKiB  int64

	MemAvailableP50KiB *int64
	MemAvailableP95KiB *int64

	DiskAllocatedMaxBytes int64

	FilesystemUsedMaxBytes  *int64
	FilesystemCapacityBytes *int64

	DiskReadIOPSP95  *int64
	DiskWriteIOPSP95 *int64
	DiskReadBPS95    *int64
	DiskWriteBPS95   *int64

	SampleCount      int32
	AgentSampleCount int32
}

// VMDigestKey identifies a single VM-day digest group.
type VMDigestKey struct {
	VMName     string
	Namespace  string
	BucketDate time.Time
}

type vmDigestAccumulator struct {
	nodeName string
	guestOS  string

	cpuUsage      []float64
	cpuRequest    float64
	cpuLimit      float64
	memUsage      []float64
	memRequest    float64
	memAvailable  []float64
	diskAllocated []float64

	fsUsed        []float64
	fsCapacity    *float64
	diskReadIOPS  []float64
	diskWriteIOPS []float64
	diskReadBPS   []float64
	diskWriteBPS  []float64

	sampleCount      int
	agentSampleCount int
}

// BuildDailyVMDigests aggregates 15-minute VM samples into daily digests keyed by
// (vm_name, namespace, bucket_date).
func BuildDailyVMDigests(rows []VMRow) map[VMDigestKey]VMDigestResult {
	groups := make(map[VMDigestKey]*vmDigestAccumulator)

	for _, r := range rows {
		bucketDate := time.Date(
			r.IntervalStart.UTC().Year(),
			r.IntervalStart.UTC().Month(),
			r.IntervalStart.UTC().Day(),
			0, 0, 0, 0, time.UTC,
		)
		key := VMDigestKey{
			VMName:     r.VMName,
			Namespace:  r.Namespace,
			BucketDate: bucketDate,
		}

		acc, ok := groups[key]
		if !ok {
			acc = &vmDigestAccumulator{}
			groups[key] = acc
		}

		if r.NodeName != "" {
			acc.nodeName = r.NodeName
		}
		if r.GuestOS != "" {
			acc.guestOS = r.GuestOS
		}

		acc.cpuUsage = append(acc.cpuUsage, r.CPUUsageMC)
		acc.cpuRequest = r.CPURequestMC
		acc.cpuLimit = r.CPULimitMC

		acc.memUsage = append(acc.memUsage, r.MemoryUsageKiB)
		acc.memRequest = r.MemoryRequestKiB
		if r.MemoryAvailableKiB != nil {
			acc.memAvailable = append(acc.memAvailable, *r.MemoryAvailableKiB)
			acc.agentSampleCount++
		}

		acc.diskAllocated = append(acc.diskAllocated, r.DiskAllocatedBytes)

		if r.FilesystemUsedBytes != nil {
			acc.fsUsed = append(acc.fsUsed, *r.FilesystemUsedBytes)
		}
		if r.FilesystemCapacityBytes != nil {
			acc.fsCapacity = r.FilesystemCapacityBytes
		}
		if r.DiskReadIOPS != nil {
			acc.diskReadIOPS = append(acc.diskReadIOPS, *r.DiskReadIOPS)
		}
		if r.DiskWriteIOPS != nil {
			acc.diskWriteIOPS = append(acc.diskWriteIOPS, *r.DiskWriteIOPS)
		}
		if r.DiskReadBytesPerSec != nil {
			acc.diskReadBPS = append(acc.diskReadBPS, *r.DiskReadBytesPerSec)
		}
		if r.DiskWriteBytesPerSec != nil {
			acc.diskWriteBPS = append(acc.diskWriteBPS, *r.DiskWriteBytesPerSec)
		}

		acc.sampleCount++
	}

	out := make(map[VMDigestKey]VMDigestResult, len(groups))
	for key, acc := range groups {
		out[key] = finalizeVMDigest(key, acc)
	}
	return out
}

func finalizeVMDigest(key VMDigestKey, acc *vmDigestAccumulator) VMDigestResult {
	d := VMDigestResult{
		VMName:      key.VMName,
		Namespace:   key.Namespace,
		NodeName:    acc.nodeName,
		GuestOS:     acc.guestOS,
		BucketDate:  key.BucketDate,
		SampleCount:      int32(acc.sampleCount),
		AgentSampleCount: int32(acc.agentSampleCount),
	}

	sortedCPU := sortedCopy(acc.cpuUsage)
	d.CPUUsageP50MC = roundFloat64ToInt64(percentileFloat(sortedCPU, 0.50))
	d.CPUUsageP95MC = roundFloat64ToInt64(percentileFloat(sortedCPU, 0.95))
	d.CPUUsageP99MC = roundFloat64ToInt64(percentileFloat(sortedCPU, 0.99))
	d.CPUUsageMaxMC = roundFloat64ToInt64(maxFloat(acc.cpuUsage))
	d.CPURequestMC = roundFloat64ToInt64(acc.cpuRequest)
	d.CPULimitMC = roundFloat64ToInt64(acc.cpuLimit)

	sortedMem := sortedCopy(acc.memUsage)
	d.MemUsageP50KiB = roundFloat64ToInt64(percentileFloat(sortedMem, 0.50))
	d.MemUsageP95KiB = roundFloat64ToInt64(percentileFloat(sortedMem, 0.95))
	d.MemUsageP99KiB = roundFloat64ToInt64(percentileFloat(sortedMem, 0.99))
	d.MemUsageMaxKiB = roundFloat64ToInt64(maxFloat(acc.memUsage))
	d.MemRequestKiB = roundFloat64ToInt64(acc.memRequest)

	const minAgentSamplesForPercentile = 20
	if acc.agentSampleCount >= minAgentSamplesForPercentile {
		sortedAvail := sortedCopy(acc.memAvailable)
		p50 := roundFloat64ToInt64(percentileFloat(sortedAvail, 0.50))
		p95 := roundFloat64ToInt64(percentileFloat(sortedAvail, 0.95))
		d.MemAvailableP50KiB = &p50
		d.MemAvailableP95KiB = &p95
	}

	d.DiskAllocatedMaxBytes = roundFloat64ToInt64(maxFloat(acc.diskAllocated))

	if len(acc.fsUsed) > 0 {
		maxUsed := roundFloat64ToInt64(maxFloat(acc.fsUsed))
		d.FilesystemUsedMaxBytes = &maxUsed
	}
	if acc.fsCapacity != nil {
		cap := roundFloat64ToInt64(*acc.fsCapacity)
		d.FilesystemCapacityBytes = &cap
	}

	if len(acc.diskReadIOPS) > 0 {
		p95 := roundFloat64ToInt64(percentileFloat(sortedCopy(acc.diskReadIOPS), 0.95))
		d.DiskReadIOPSP95 = &p95
	}
	if len(acc.diskWriteIOPS) > 0 {
		p95 := roundFloat64ToInt64(percentileFloat(sortedCopy(acc.diskWriteIOPS), 0.95))
		d.DiskWriteIOPSP95 = &p95
	}
	if len(acc.diskReadBPS) > 0 {
		p95 := roundFloat64ToInt64(percentileFloat(sortedCopy(acc.diskReadBPS), 0.95))
		d.DiskReadBPS95 = &p95
	}
	if len(acc.diskWriteBPS) > 0 {
		p95 := roundFloat64ToInt64(percentileFloat(sortedCopy(acc.diskWriteBPS), 0.95))
		d.DiskWriteBPS95 = &p95
	}

	return d
}

func sortedCopy(values []float64) []float64 {
	if len(values) == 0 {
		return nil
	}
	out := slices.Clone(values)
	slices.Sort(out)
	return out
}

func percentileFloat(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(sorted))*p)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func maxFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
}

func roundFloat64ToInt64(v float64) int64 {
	return int64(math.Round(v))
}

// ProcessVMCSV parses VM usage CSV, builds daily digests, and upserts them.
func ProcessVMCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ParseVMCSVRows(r)
	if err != nil {
		return fmt.Errorf("parsing VM CSV: %w", err)
	}
	if len(rows) == 0 {
		logging.ForOrg(orgID, clusterUUID).Info("ProcessVMCSV: no VM rows found")
		return nil
	}

	digestMap := BuildDailyVMDigests(rows)
	digests := make([]VMDigestResult, 0, len(digestMap))
	for _, d := range digestMap {
		digests = append(digests, d)
	}

	if err := UpsertDailyVMDigests(ctx, pool, orgID, clusterUUID, digests); err != nil {
		return fmt.Errorf("upserting VM digests: %w", err)
	}

	logging.ForOrg(orgID, clusterUUID).Infof("ProcessVMCSV: upserted %d VM digests", len(digests))
	return nil
}
