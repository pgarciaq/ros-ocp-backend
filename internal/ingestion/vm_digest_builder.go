package ingestion

import (
	"context"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// ingestGPUDeviceDigest holds per-GPU metrics for digest upsert (avoids model import cycle).
type ingestGPUDeviceDigest struct {
	UUID          string
	Model         string
	UtilAvgBP     int32
	UtilMaxBP     int32
	FBUsedAvgMiB  float64
	FBUsedMaxMiB  float64
	SMActiveAvgBP int32
	TensorAvgBP   int32
	DRAMAvgBP     int32
	MIGProfile    string
	MaxSlices     int32
}

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
	RestartCountSum  int32

	GPUCount         int32
	GPUModel         string
	GPUUtilAvgBP     int32
	GPUUtilMaxBP     int32
	GPUFBUsedAvgMiB  float64
	GPUFBUsedMaxMiB  float64
	GPUSMActiveAvgBP int32
	GPUTensorAvgBP   int32
	GPUDRAMAvgBP     int32
	GPUMIGProfile    string
	GPUMaxSlices     int32
	HasGPU           bool
	GPUDevices       []ingestGPUDeviceDigest

	NetThroughputP95BPS int64
	NetPPSP95           int64
	NetDropRatioMaxBP   int32
}

func vmBucketDate(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
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
	restartCountSum  int32

	gpuCountSamples int32
	gpuModel        string
	gpuUtilAvg      []float64
	gpuUtilMax      []float64
	gpuFBAvg        []float64
	gpuFBMax        []float64
	gpuSMAvg        []float64
	gpuTensorAvg    []float64
	gpuDRAMAvg      []float64
	gpuMIGProfile   string
	gpuMaxSlices    int32

	gpuDevices map[string]*vmGPUDeviceAccumulator

	netThroughput []float64
	netPPS        []float64
	netDropBP     []int32
}

type vmGPUDeviceAccumulator struct {
	uuid       string
	model      string
	migProfile string
	maxSlices  int32
	utilAvg    []float64
	utilMax    []float64
	fbAvg      []float64
	fbMax      []float64
	smAvg      []float64
	tensorAvg  []float64
	dramAvg    []float64
}

// BuildDailyVMDigests aggregates 15-minute VM samples into daily digests keyed by
// (vm_name, namespace, bucket_date).
func BuildDailyVMDigests(rows []VMRow) map[VMDigestKey]VMDigestResult {
	return buildDailyVMDigests(rows, nil)
}

// BuildDailyVMDigestsIfWeight includes a sample when weightFn returns > 0.
// Weight is drop-or-full: the sample is never fractionally scaled into percentiles.
func BuildDailyVMDigestsIfWeight(rows []VMRow, weightFn func(VMRow) float64) map[VMDigestKey]VMDigestResult {
	return buildDailyVMDigests(rows, weightFn)
}

func buildDailyVMDigests(rows []VMRow, weightFn func(VMRow) float64) map[VMDigestKey]VMDigestResult {
	groups := make(map[VMDigestKey]*vmDigestAccumulator)
	for _, r := range rows {
		addVMRowToDigestGroups(groups, r, weightFn)
	}
	return finalizeVMDigestGroups(groups)
}

func addVMRowToDigestGroups(groups map[VMDigestKey]*vmDigestAccumulator, r VMRow, weightFn func(VMRow) float64) {
	if weightFn != nil && weightFn(r) <= 0 {
		return
	}
	bucketDate := vmBucketDate(r.IntervalStart)
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

	if r.RestartCount != nil {
		acc.restartCountSum += *r.RestartCount
	}

	if r.GPUCount != nil && *r.GPUCount > 0 {
		acc.gpuCountSamples = maxInt32(acc.gpuCountSamples, *r.GPUCount)
		acc.ingestGPURow(r)
	}

	acc.ingestNetworkRow(r)

	acc.sampleCount++
}

func finalizeVMDigestGroups(groups map[VMDigestKey]*vmDigestAccumulator) map[VMDigestKey]VMDigestResult {
	out := make(map[VMDigestKey]VMDigestResult, len(groups))
	for key, acc := range groups {
		out[key] = finalizeVMDigest(key, acc)
	}
	return out
}

func finalizeVMDigest(key VMDigestKey, acc *vmDigestAccumulator) VMDigestResult {
	d := VMDigestResult{
		VMName:           key.VMName,
		Namespace:        key.Namespace,
		NodeName:         acc.nodeName,
		GuestOS:          acc.guestOS,
		BucketDate:       key.BucketDate,
		SampleCount:      int32(acc.sampleCount),
		AgentSampleCount: int32(acc.agentSampleCount),
		RestartCountSum:  acc.restartCountSum,
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

	if len(acc.netThroughput) > 0 {
		d.NetThroughputP95BPS = roundFloat64ToInt64(percentileFloat(sortedCopy(acc.netThroughput), 0.95))
	}
	if len(acc.netPPS) > 0 {
		d.NetPPSP95 = roundFloat64ToInt64(percentileFloat(sortedCopy(acc.netPPS), 0.95))
	}
	if len(acc.netDropBP) > 0 {
		d.NetDropRatioMaxBP = maxInt32Slice(acc.netDropBP)
	}

	if acc.gpuCountSamples > 0 {
		d.HasGPU = true
		d.GPUCount = acc.gpuCountSamples
		d.GPUModel = acc.gpuModel
		d.GPUUtilAvgBP = ratioToBasisPoints(avgFloatSlice(acc.gpuUtilAvg))
		d.GPUUtilMaxBP = ratioToBasisPoints(maxFloatSlice(acc.gpuUtilMax))
		d.GPUFBUsedAvgMiB = avgFloatSlice(acc.gpuFBAvg)
		d.GPUFBUsedMaxMiB = maxFloatSlice(acc.gpuFBMax)
		d.GPUSMActiveAvgBP = ratioToBasisPoints(avgFloatSlice(acc.gpuSMAvg))
		d.GPUTensorAvgBP = ratioToBasisPoints(avgFloatSlice(acc.gpuTensorAvg))
		d.GPUDRAMAvgBP = ratioToBasisPoints(avgFloatSlice(acc.gpuDRAMAvg))
		d.GPUMIGProfile = acc.gpuMIGProfile
		d.GPUMaxSlices = acc.gpuMaxSlices
		d.GPUDevices = acc.finalizeGPUDevices()
	}

	return d
}

func (acc *vmDigestAccumulator) ingestNetworkRow(r VMRow) {
	var rxBps, txBps, rxPps, txPps, rxDrop, txDrop float64
	hasBytes := r.NetRxBytesPerSec != nil || r.NetTxBytesPerSec != nil
	hasPackets := r.NetRxPacketsPerSec != nil || r.NetTxPacketsPerSec != nil
	if !hasBytes && !hasPackets {
		return
	}
	if r.NetRxBytesPerSec != nil {
		rxBps = *r.NetRxBytesPerSec
	}
	if r.NetTxBytesPerSec != nil {
		txBps = *r.NetTxBytesPerSec
	}
	acc.netThroughput = append(acc.netThroughput, rxBps+txBps)

	if r.NetRxPacketsPerSec != nil {
		rxPps = *r.NetRxPacketsPerSec
	}
	if r.NetTxPacketsPerSec != nil {
		txPps = *r.NetTxPacketsPerSec
	}
	acc.netPPS = append(acc.netPPS, rxPps+txPps)

	if r.NetRxDropsPerSec != nil {
		rxDrop = *r.NetRxDropsPerSec
	}
	if r.NetTxDropsPerSec != nil {
		txDrop = *r.NetTxDropsPerSec
	}
	totalPackets := rxPps + txPps
	if totalPackets > 0 {
		dropRatioBP := int32(math.Round((rxDrop + txDrop) / totalPackets * 10000))
		if dropRatioBP < 0 {
			dropRatioBP = 0
		}
		acc.netDropBP = append(acc.netDropBP, dropRatioBP)
	}
}

func (acc *vmDigestAccumulator) ingestGPURow(r VMRow) {
	if r.GPUModel != nil && *r.GPUModel != "" {
		acc.gpuModel = *r.GPUModel
	}
	if r.GPUUtilizationAvg != nil {
		acc.gpuUtilAvg = append(acc.gpuUtilAvg, *r.GPUUtilizationAvg)
	}
	if r.GPUUtilizationMax != nil {
		acc.gpuUtilMax = append(acc.gpuUtilMax, *r.GPUUtilizationMax)
	}
	if r.GPUFBUsedAvgMiB != nil {
		acc.gpuFBAvg = append(acc.gpuFBAvg, *r.GPUFBUsedAvgMiB)
	}
	if r.GPUFBUsedMaxMiB != nil {
		acc.gpuFBMax = append(acc.gpuFBMax, *r.GPUFBUsedMaxMiB)
	}
	if r.GPUSMActiveAvg != nil {
		acc.gpuSMAvg = append(acc.gpuSMAvg, *r.GPUSMActiveAvg)
	}
	if r.GPUTensorActiveAvg != nil {
		acc.gpuTensorAvg = append(acc.gpuTensorAvg, *r.GPUTensorActiveAvg)
	}
	if r.GPUDRAMActiveAvg != nil {
		acc.gpuDRAMAvg = append(acc.gpuDRAMAvg, *r.GPUDRAMActiveAvg)
	}
	if r.GPUMIGProfile != nil && *r.GPUMIGProfile != "" {
		acc.gpuMIGProfile = *r.GPUMIGProfile
	}
	if r.GPUMaxSlices != nil && *r.GPUMaxSlices > acc.gpuMaxSlices {
		acc.gpuMaxSlices = *r.GPUMaxSlices
	}

	uuid := "gpu-0"
	if r.GPUUUID != nil && strings.TrimSpace(*r.GPUUUID) != "" {
		uuid = strings.TrimSpace(*r.GPUUUID)
	}
	if acc.gpuDevices == nil {
		acc.gpuDevices = make(map[string]*vmGPUDeviceAccumulator)
	}
	dev, ok := acc.gpuDevices[uuid]
	if !ok {
		dev = &vmGPUDeviceAccumulator{uuid: uuid}
		acc.gpuDevices[uuid] = dev
	}
	if r.GPUModel != nil && *r.GPUModel != "" {
		dev.model = *r.GPUModel
	}
	if r.GPUUtilizationAvg != nil {
		dev.utilAvg = append(dev.utilAvg, *r.GPUUtilizationAvg)
	}
	if r.GPUUtilizationMax != nil {
		dev.utilMax = append(dev.utilMax, *r.GPUUtilizationMax)
	}
	if r.GPUFBUsedAvgMiB != nil {
		dev.fbAvg = append(dev.fbAvg, *r.GPUFBUsedAvgMiB)
	}
	if r.GPUFBUsedMaxMiB != nil {
		dev.fbMax = append(dev.fbMax, *r.GPUFBUsedMaxMiB)
	}
	if r.GPUSMActiveAvg != nil {
		dev.smAvg = append(dev.smAvg, *r.GPUSMActiveAvg)
	}
	if r.GPUTensorActiveAvg != nil {
		dev.tensorAvg = append(dev.tensorAvg, *r.GPUTensorActiveAvg)
	}
	if r.GPUDRAMActiveAvg != nil {
		dev.dramAvg = append(dev.dramAvg, *r.GPUDRAMActiveAvg)
	}
	if r.GPUMIGProfile != nil && *r.GPUMIGProfile != "" {
		dev.migProfile = *r.GPUMIGProfile
	}
	if r.GPUMaxSlices != nil && *r.GPUMaxSlices > dev.maxSlices {
		dev.maxSlices = *r.GPUMaxSlices
	}
}

func (acc *vmDigestAccumulator) finalizeGPUDevices() []ingestGPUDeviceDigest {
	if len(acc.gpuDevices) == 0 {
		return nil
	}
	devs := make([]ingestGPUDeviceDigest, 0, len(acc.gpuDevices))
	for _, d := range acc.gpuDevices {
		devs = append(devs, d.toIngestGPUDeviceDigest())
	}
	return devs
}

func maxInt32(a, b int32) int32 {
	if b > a {
		return b
	}
	return a
}

func maxInt32Slice(values []int32) int32 {
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

func avgFloatSlice(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func maxFloatSlice(values []float64) float64 {
	return maxFloat(values)
}

func ratioToBasisPoints(r float64) int32 {
	if r < 0 {
		return 0
	}
	return int32(math.Round(r * 10000))
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

func digestResultsSlice(digestMap map[VMDigestKey]VMDigestResult) []VMDigestResult {
	digests := make([]VMDigestResult, 0, len(digestMap))
	for _, d := range digestMap {
		digests = append(digests, d)
	}
	return digests
}

// ProcessVMCSV parses VM usage CSV, builds daily digests, and upserts them.
// It streams rows into digest accumulators and does not retain a []VMRow of
// every 15-minute sample. Business-hours and hourly digests are accumulated
// in the same pass when enabled.
func ProcessVMCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	dailyGroups := make(map[VMDigestKey]*vmDigestAccumulator)
	var bhGroups map[VMDigestKey]*vmDigestAccumulator
	var hourlyGroups map[VMHourlyDigestKey]*vmHourlyAccumulator

	var cache *bhschedule.Cache
	if BusinessHoursAggregationEnabled() {
		var loadErr error
		cache, loadErr = bhschedule.LoadSchedules(ctx, pool, orgID, clusterUUID)
		if loadErr != nil {
			return fmt.Errorf("load business hours schedules for VM ingest: %w", loadErr)
		}
		if cache != nil && cache.ProducesBusinessHoursDigests() {
			bhGroups = make(map[VMDigestKey]*vmDigestAccumulator)
		}
	}
	if config.HourlyVMDigestsEnabled() {
		hourlyGroups = make(map[VMHourlyDigestKey]*vmHourlyAccumulator)
	}

	n := 0
	_, err := forEachVMCSVRow(ctx, r, func(row VMRow) error {
		addVMRowToDigestGroups(dailyGroups, row, nil)
		if bhGroups != nil {
			addVMRowToDigestGroups(bhGroups, row, VMBusinessHoursWeight(cache))
		}
		if hourlyGroups != nil {
			addVMRowToHourlyGroups(hourlyGroups, row)
		}
		n++
		return nil
	})
	if err != nil {
		return fmt.Errorf("parsing VM CSV: %w", err)
	}
	if n == 0 {
		logging.ForOrg(orgID, clusterUUID).Info("ProcessVMCSV: no VM rows found")
		return nil
	}

	digests := digestResultsSlice(finalizeVMDigestGroups(dailyGroups))
	if err := UpsertDailyVMDigests(ctx, pool, orgID, clusterUUID, digests); err != nil {
		return fmt.Errorf("upserting VM digests: %w", err)
	}
	logging.ForOrg(orgID, clusterUUID).Infof("ProcessVMCSV: upserted %d VM digests", len(digests))

	if BusinessHoursAggregationEnabled() && bhGroups == nil {
		if err := bhschedule.PruneClusterVMBusinessHoursDigests(ctx, pool, orgID, clusterUUID); err != nil {
			return err
		}
	} else if bhGroups != nil {
		bhDigests := digestResultsSlice(finalizeVMDigestGroups(bhGroups))
		if len(bhDigests) > 0 {
			if err := UpsertDailyVMDigestsWithSchedule(ctx, pool, orgID, clusterUUID, string(ScheduleTypeBusinessHours), bhDigests); err != nil {
				return fmt.Errorf("upserting VM business_hours digests: %w", err)
			}
			logging.ForOrg(orgID, clusterUUID).Infof("ProcessVMCSV: upserted %d VM business_hours digests", len(bhDigests))
		}
	}

	if hourlyGroups != nil {
		hourlyMap := finalizeHourlyVMGroups(hourlyGroups)
		if err := UpsertHourlyVMDigests(ctx, pool, orgID, clusterUUID, hourlyMap); err != nil {
			return fmt.Errorf("upserting hourly VM digests: %w", err)
		}
		logging.ForOrg(orgID, clusterUUID).Infof("ProcessVMCSV: upserted %d hourly VM digests", len(hourlyMap))
	}

	return nil
}
