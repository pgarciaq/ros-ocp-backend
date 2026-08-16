package csv

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/vm"
)

const vmMinAgentSamplesForPercentile = 20

type vmDigestKey struct {
	VMName     string
	Namespace  string
	BucketDate time.Time
}

// VMDataset is daily VM aggregation metadata (MaxEnd is the engine clock fallback).
type VMDataset struct {
	MaxEnd time.Time
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
	gpuDevices      map[string]*vmGPUDeviceAccumulator

	netThroughput []float64
	netPPS        []float64
	netDropBP     []int32

	pvcs []vm.PVCDigest
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

func vmBucketDate(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// DailyVMDigests aggregates VM usage rows into per-VM daily digests and optionally
// attaches PVC / GPU companion samples onto matching usage days.
func DailyVMDigests(rows []VMRow, pvcRows []VMPVCRow, gpuRows []VMGPURow) ([]vm.DailyVMDigest, VMDataset) {
	var ds VMDataset
	if len(rows) == 0 {
		return nil, ds
	}

	groups := make(map[vmDigestKey]*vmDigestAccumulator)
	for _, r := range rows {
		end := r.IntervalEnd
		if end.IsZero() {
			end = r.IntervalStart
		}
		if ds.MaxEnd.IsZero() || end.After(ds.MaxEnd) {
			ds.MaxEnd = end
		}
		key := vmDigestKey{
			VMName:     r.VMName,
			Namespace:  r.Namespace,
			BucketDate: vmBucketDate(r.IntervalStart),
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

	mergeVMPVCIntoAcc(groups, pvcRows)
	mergeVMGPUIntoAcc(groups, gpuRows)

	out := make([]vm.DailyVMDigest, 0, len(groups))
	for key, acc := range groups {
		out = append(out, finalizeVMDigest(key, acc))
	}
	slices.SortFunc(out, func(a, b vm.DailyVMDigest) int {
		if n := cmp.Compare(a.Namespace, b.Namespace); n != 0 {
			return n
		}
		if n := cmp.Compare(a.VMName, b.VMName); n != 0 {
			return n
		}
		return cmp.Compare(a.BucketDate.Unix(), b.BucketDate.Unix())
	})
	return out, ds
}

func finalizeVMDigest(key vmDigestKey, acc *vmDigestAccumulator) vm.DailyVMDigest {
	d := vm.DailyVMDigest{
		VMName:           key.VMName,
		Namespace:        key.Namespace,
		NodeName:         acc.nodeName,
		GuestOS:          acc.guestOS,
		BucketDate:       key.BucketDate,
		SampleCount:      int32(acc.sampleCount),
		AgentSampleCount: int32(acc.agentSampleCount),
		RestartCountSum:  acc.restartCountSum,
		PVCs:             acc.pvcs,
	}

	sortedCPU := sortedCopyFloat(acc.cpuUsage)
	d.CPUUsageP50MC = roundFloat64ToInt64(percentileFloat(sortedCPU, 0.50))
	d.CPUUsageP95MC = roundFloat64ToInt64(percentileFloat(sortedCPU, 0.95))
	d.CPUUsageP99MC = roundFloat64ToInt64(percentileFloat(sortedCPU, 0.99))
	d.CPUUsageMaxMC = roundFloat64ToInt64(maxFloat64(acc.cpuUsage))
	d.CPURequestMC = roundFloat64ToInt64(acc.cpuRequest)
	d.CPULimitMC = roundFloat64ToInt64(acc.cpuLimit)

	sortedMem := sortedCopyFloat(acc.memUsage)
	d.MemUsageP50KiB = roundFloat64ToInt64(percentileFloat(sortedMem, 0.50))
	d.MemUsageP95KiB = roundFloat64ToInt64(percentileFloat(sortedMem, 0.95))
	d.MemUsageP99KiB = roundFloat64ToInt64(percentileFloat(sortedMem, 0.99))
	d.MemUsageMaxKiB = roundFloat64ToInt64(maxFloat64(acc.memUsage))
	d.MemRequestKiB = roundFloat64ToInt64(acc.memRequest)

	if acc.agentSampleCount >= vmMinAgentSamplesForPercentile {
		sortedAvail := sortedCopyFloat(acc.memAvailable)
		p50 := roundFloat64ToInt64(percentileFloat(sortedAvail, 0.50))
		p95 := roundFloat64ToInt64(percentileFloat(sortedAvail, 0.95))
		d.MemAvailableP50KiB = &p50
		d.MemAvailableP95KiB = &p95
	}

	d.DiskAllocatedMaxBytes = roundFloat64ToInt64(maxFloat64(acc.diskAllocated))
	if len(acc.fsUsed) > 0 {
		maxUsed := roundFloat64ToInt64(maxFloat64(acc.fsUsed))
		d.FilesystemUsedMaxBytes = &maxUsed
	}
	if acc.fsCapacity != nil {
		cap := roundFloat64ToInt64(*acc.fsCapacity)
		d.FilesystemCapacityBytes = &cap
	}
	if len(acc.diskReadIOPS) > 0 {
		p95 := roundFloat64ToInt64(percentileFloat(sortedCopyFloat(acc.diskReadIOPS), 0.95))
		d.DiskReadIOPSP95 = &p95
	}
	if len(acc.diskWriteIOPS) > 0 {
		p95 := roundFloat64ToInt64(percentileFloat(sortedCopyFloat(acc.diskWriteIOPS), 0.95))
		d.DiskWriteIOPSP95 = &p95
	}
	if len(acc.diskReadBPS) > 0 {
		p95 := roundFloat64ToInt64(percentileFloat(sortedCopyFloat(acc.diskReadBPS), 0.95))
		d.DiskReadBPS95 = &p95
	}
	if len(acc.diskWriteBPS) > 0 {
		p95 := roundFloat64ToInt64(percentileFloat(sortedCopyFloat(acc.diskWriteBPS), 0.95))
		d.DiskWriteBPS95 = &p95
	}
	if len(acc.netThroughput) > 0 {
		d.NetThroughputP95BPS = roundFloat64ToInt64(percentileFloat(sortedCopyFloat(acc.netThroughput), 0.95))
	}
	if len(acc.netPPS) > 0 {
		d.NetPPSP95 = roundFloat64ToInt64(percentileFloat(sortedCopyFloat(acc.netPPS), 0.95))
	}
	if len(acc.netDropBP) > 0 {
		d.NetDropRatioMaxBP = maxInt32Slice(acc.netDropBP)
	}

	if acc.gpuCountSamples > 0 || len(acc.gpuDevices) > 0 {
		d.HasGPU = true
		d.GPUCount = acc.gpuCountSamples
		if d.GPUCount == 0 {
			d.GPUCount = int32(len(acc.gpuDevices))
		}
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
		d.Devices = acc.finalizeGPUDevices()
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

func (acc *vmDigestAccumulator) finalizeGPUDevices() []vm.GPUDeviceDigest {
	if len(acc.gpuDevices) == 0 {
		return nil
	}
	devs := make([]vm.GPUDeviceDigest, 0, len(acc.gpuDevices))
	for _, d := range acc.gpuDevices {
		devs = append(devs, d.toGPUDeviceDigest())
	}
	slices.SortFunc(devs, func(a, b vm.GPUDeviceDigest) int {
		return cmp.Compare(a.UUID, b.UUID)
	})
	return devs
}

func (d *vmGPUDeviceAccumulator) toGPUDeviceDigest() vm.GPUDeviceDigest {
	return vm.GPUDeviceDigest{
		UUID:          d.uuid,
		Model:         d.model,
		UtilAvgBP:     ratioToBasisPoints(avgFloatSlice(d.utilAvg)),
		UtilMaxBP:     ratioToBasisPoints(maxFloatSlice(d.utilMax)),
		FBUsedAvgMiB:  avgFloatSlice(d.fbAvg),
		FBUsedMaxMiB:  maxFloatSlice(d.fbMax),
		SMActiveAvgBP: ratioToBasisPoints(avgFloatSlice(d.smAvg)),
		TensorAvgBP:   ratioToBasisPoints(avgFloatSlice(d.tensorAvg)),
		DRAMAvgBP:     ratioToBasisPoints(avgFloatSlice(d.dramAvg)),
		MIGProfile:    d.migProfile,
		MaxSlices:     d.maxSlices,
	}
}

func mergeVMPVCIntoAcc(groups map[vmDigestKey]*vmDigestAccumulator, rows []VMPVCRow) {
	type pvcKey struct {
		digest  vmDigestKey
		pvcName string
	}
	acc := make(map[pvcKey]*vm.PVCDigest)
	for _, r := range rows {
		dk := vmDigestKey{VMName: r.VMName, Namespace: r.Namespace, BucketDate: vmBucketDate(r.IntervalStart)}
		if _, ok := groups[dk]; !ok {
			continue
		}
		key := pvcKey{digest: dk, pvcName: r.PVCName}
		p, ok := acc[key]
		if !ok {
			p = &vm.PVCDigest{
				PVCName:           r.PVCName,
				DiskCapacityBytes: r.DiskCapacityBytes,
				VolumeMode:        r.VolumeMode,
			}
			acc[key] = p
		}
		if r.DiskCapacityBytes > p.DiskCapacityBytes {
			p.DiskCapacityBytes = r.DiskCapacityBytes
		}
		if r.VolumeMode != "" {
			p.VolumeMode = r.VolumeMode
		}
	}
	for key, p := range acc {
		groups[key.digest].pvcs = append(groups[key.digest].pvcs, *p)
	}
}

func mergeVMGPUIntoAcc(groups map[vmDigestKey]*vmDigestAccumulator, rows []VMGPURow) {
	type deviceKey struct {
		digest vmDigestKey
		uuid   string
	}
	acc := make(map[deviceKey]*vmGPUDeviceAccumulator)
	for _, r := range rows {
		dk := vmDigestKey{VMName: r.VMName, Namespace: r.Namespace, BucketDate: vmBucketDate(r.IntervalStart)}
		if _, ok := groups[dk]; !ok {
			continue
		}
		key := deviceKey{digest: dk, uuid: r.GPUUUID}
		dev, ok := acc[key]
		if !ok {
			dev = &vmGPUDeviceAccumulator{uuid: r.GPUUUID, model: r.GPUModel, maxSlices: r.MaxSlices, migProfile: r.MIGProfile}
			acc[key] = dev
		}
		if r.GPUModel != "" {
			dev.model = r.GPUModel
		}
		if r.UtilizationAvg > 0 {
			dev.utilAvg = append(dev.utilAvg, r.UtilizationAvg)
		}
		if r.UtilizationMax > 0 {
			dev.utilMax = append(dev.utilMax, r.UtilizationMax)
		}
		if r.FBUsedAvgMiB > 0 {
			dev.fbAvg = append(dev.fbAvg, r.FBUsedAvgMiB)
		}
		if r.FBUsedMaxMiB > 0 {
			dev.fbMax = append(dev.fbMax, r.FBUsedMaxMiB)
		}
		if r.SMActiveAvg > 0 {
			dev.smAvg = append(dev.smAvg, r.SMActiveAvg)
		}
		if r.TensorActiveAvg > 0 {
			dev.tensorAvg = append(dev.tensorAvg, r.TensorActiveAvg)
		}
		if r.DRAMActiveAvg > 0 {
			dev.dramAvg = append(dev.dramAvg, r.DRAMActiveAvg)
		}
		if r.MIGProfile != "" {
			dev.migProfile = r.MIGProfile
		}
		if r.MaxSlices > dev.maxSlices {
			dev.maxSlices = r.MaxSlices
		}
	}
	for key, dev := range acc {
		g := groups[key.digest]
		if g.gpuDevices == nil {
			g.gpuDevices = make(map[string]*vmGPUDeviceAccumulator)
		}
		g.gpuDevices[dev.uuid] = dev
		if g.gpuCountSamples == 0 {
			g.gpuCountSamples = int32(len(g.gpuDevices))
		}
		if dev.model != "" {
			g.gpuModel = dev.model
		}
		if dev.maxSlices > g.gpuMaxSlices {
			g.gpuMaxSlices = dev.maxSlices
		}
		if dev.migProfile != "" {
			g.gpuMIGProfile = dev.migProfile
		}
	}
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
	return maxFloat64(values)
}

func ratioToBasisPoints(r float64) int32 {
	if r < 0 {
		return 0
	}
	return int32(math.Round(r * 10000))
}

func sortedCopyFloat(values []float64) []float64 {
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

func maxFloat64(values []float64) float64 {
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
