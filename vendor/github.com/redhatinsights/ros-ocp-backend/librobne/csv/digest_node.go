package csv

import (
	"cmp"
	"slices"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/node"
)

const nodeDayHours = 24

type nodeDayKey struct {
	node string
	day  time.Time
}

type nodeDayAccumulator struct {
	intervalCPUReqs      [nodeDayHours]int64
	intervalMemReqs      [nodeDayHours]int64
	intervalCPUUse       [nodeDayHours]int64
	intervalMemUse       [nodeDayHours]int64
	intervalPodsDistinct [nodeDayHours]map[string]struct{}
	intervalSeen         [nodeDayHours]bool
	MaxCPUCapacityMC     int64
	MaxMemCapacityKiB    int64
	MaxCPUAllocatableMC  int64
	MaxMemAllocatableKiB int64
	MaxPodCapacity       int64
	MaxGPUAllocatable    int64
	InstanceType         string
	MachineSetName       string
}

func hourIndex(t time.Time) int {
	return t.UTC().Hour()
}

func (a *nodeDayAccumulator) add(r Row) {
	a.addWeighted(r, 1)
}

func scaleInt64(v int64, weight float64) int64 {
	if weight == 1 {
		return v
	}
	return int64(float64(v) * weight)
}

func (a *nodeDayAccumulator) addWeighted(r Row, weight float64) {
	if weight <= 0 {
		return
	}
	h := hourIndex(r.IntervalStart)
	a.intervalSeen[h] = true
	a.intervalCPUReqs[h] += scaleInt64(r.CPURequestMC, weight)
	a.intervalMemReqs[h] += scaleInt64(r.MemRequestKiB, weight)
	a.intervalCPUUse[h] += scaleInt64(r.CPUUsageMC, weight)
	a.intervalMemUse[h] += scaleInt64(r.MemUsageKiB, weight)
	if r.Pod != "" {
		if a.intervalPodsDistinct[h] == nil {
			a.intervalPodsDistinct[h] = make(map[string]struct{})
		}
		a.intervalPodsDistinct[h][r.Pod] = struct{}{}
	}
	if r.NodeCapacityCPUMC > a.MaxCPUCapacityMC {
		a.MaxCPUCapacityMC = r.NodeCapacityCPUMC
	}
	if r.NodeCapacityMemKiB > a.MaxMemCapacityKiB {
		a.MaxMemCapacityKiB = r.NodeCapacityMemKiB
	}
	if r.NodeAllocatableCPUMC > a.MaxCPUAllocatableMC {
		a.MaxCPUAllocatableMC = r.NodeAllocatableCPUMC
	}
	if r.NodeAllocatableMemKiB > a.MaxMemAllocatableKiB {
		a.MaxMemAllocatableKiB = r.NodeAllocatableMemKiB
	}
	if a.InstanceType == "" && r.InstanceType != "" {
		a.InstanceType = r.InstanceType
	}
	if r.NodePodCapacity > a.MaxPodCapacity {
		a.MaxPodCapacity = r.NodePodCapacity
	}
	if a.MachineSetName == "" && r.MachineSetName != "" {
		a.MachineSetName = r.MachineSetName
	}
	if r.NodeAllocatableGPUCount > a.MaxGPUAllocatable {
		a.MaxGPUAllocatable = r.NodeAllocatableGPUCount
	}
}

func (a *nodeDayAccumulator) finalize() (cpuP50, cpuP95, cpuMax, memP50, memP95, memMax, maxCPUReq, maxMemReq, maxPods, sampleCount int64) {
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
		if n := int64(len(a.intervalPodsDistinct[h])); n > maxPods {
			maxPods = n
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
	cpuMax = cpuUsageSamples[len(cpuUsageSamples)-1]
	memP50 = percentileInt64(memUsageSamples, 0.50)
	memP95 = percentileInt64(memUsageSamples, 0.95)
	memMax = memUsageSamples[len(memUsageSamples)-1]
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

func nodeDigestAllocatable(observedAlloc, maxCapacity int64, factor float64) *int64 {
	if observedAlloc > 0 {
		v := observedAlloc
		return &v
	}
	if maxCapacity > 0 {
		v := int64(float64(maxCapacity) * factor)
		return &v
	}
	return nil
}

func nodeGPUCount(maxGPU int64) *int64 {
	if maxGPU > 0 {
		return &maxGPU
	}
	return nil
}

// DailyNodeDigests groups container ROS rows by (node, day). Rows with an empty
// Node are skipped. Missing allocatable falls back to capacity * allocatableFactor
// (processor default 0.93). Matches internal/ingestion node aggregation math.
func DailyNodeDigests(rows []Row, allocatableFactor float64) []node.DigestRow {
	return DailyNodeDigestsWeighted(rows, allocatableFactor, nil)
}

// DailyNodeDigestsWeighted is DailyNodeDigests with optional per-sample W_schedule.
// Weight <= 0 drops the row. Fractional weights scale usage/request adds only;
// capacity and allocatable maxes stay unscaled. weightFn nil matches DailyNodeDigests.
func DailyNodeDigestsWeighted(rows []Row, allocatableFactor float64, weightFn SampleWeightFunc) []node.DigestRow {
	accs := make(map[nodeDayKey]*nodeDayAccumulator)
	for _, r := range rows {
		if r.Node == "" {
			continue
		}
		w := 1.0
		if weightFn != nil {
			w = weightFn(r.IntervalStart)
		}
		if w <= 0 {
			continue
		}
		day := time.Date(r.IntervalStart.Year(), r.IntervalStart.Month(), r.IntervalStart.Day(), 0, 0, 0, 0, time.UTC)
		key := nodeDayKey{node: r.Node, day: day}
		acc, ok := accs[key]
		if !ok {
			acc = &nodeDayAccumulator{}
			accs[key] = acc
		}
		acc.addWeighted(r, w)
	}
	out := make([]node.DigestRow, 0, len(accs))
	for key, acc := range accs {
		cpuP50, cpuP95, cpuMax, memP50, memP95, memMax, maxCPUReq, maxMemReq, maxPods, sampleCount := acc.finalize()
		if sampleCount == 0 {
			continue
		}
		out = append(out, node.DigestRow{
			BucketDate:        key.day,
			Node:              key.node,
			CPUUsageP50MC:     cpuP50,
			CPUUsageP95MC:     cpuP95,
			CPUUsageMaxMC:     cpuMax,
			MemUsageP50KiB:    memP50,
			MemUsageP95KiB:    memP95,
			MemUsageMaxKiB:    memMax,
			MaxCPUAllocMC:     nodeDigestAllocatable(acc.MaxCPUAllocatableMC, acc.MaxCPUCapacityMC, allocatableFactor),
			MaxMemAllocKiB:    nodeDigestAllocatable(acc.MaxMemAllocatableKiB, acc.MaxMemCapacityKiB, allocatableFactor),
			MaxCPURequestsMC:  maxCPUReq,
			MaxMemRequestsKiB: maxMemReq,
			MaxPodCount:       maxPods,
			PodCapacity:       acc.MaxPodCapacity,
			InstanceType:      acc.InstanceType,
			MachineSetName:    acc.MachineSetName,
			SampleCount:       sampleCount,
			NodeGPUCount:      nodeGPUCount(acc.MaxGPUAllocatable),
		})
	}
	slices.SortFunc(out, func(a, b node.DigestRow) int {
		if c := cmp.Compare(a.Node, b.Node); c != 0 {
			return c
		}
		if a.BucketDate.Before(b.BucketDate) {
			return -1
		}
		if a.BucketDate.After(b.BucketDate) {
			return 1
		}
		return 0
	})
	return out
}
