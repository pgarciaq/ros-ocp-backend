package csv

import (
	"math"
	"slices"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/fixedpoint"
	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
)

type gpuDayKey struct {
	date      time.Time
	namespace string
	workload  string
	container string
}

type gpuDayAgg struct {
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

// GPUDataset is daily GPU digests plus last-seen node maps for timeslicing.
type GPUDataset struct {
	Grouped      map[gpu.GPUContainerKey][]gpu.GPUDigestRow
	NodeMap      map[gpu.GPUContainerKey]string
	NodeLastSeen map[string]time.Time
}

// DailyGPUDigests groups container ROS rows that carry a GPU model name.
// Rows without accelerator_model_name are skipped. GPUCount is distinct
// gpu_uuid that day, or 1 when the column is absent.
func DailyGPUDigests(rows []Row) GPUDataset {
	ds := GPUDataset{
		Grouped:      map[gpu.GPUContainerKey][]gpu.GPUDigestRow{},
		NodeMap:      map[gpu.GPUContainerKey]string{},
		NodeLastSeen: map[string]time.Time{},
	}
	groups := make(map[gpuDayKey]*gpuDayAgg)
	for _, r := range rows {
		if !r.HasGPU() {
			continue
		}
		day := time.Date(r.IntervalStart.Year(), r.IntervalStart.Month(), r.IntervalStart.Day(), 0, 0, 0, 0, time.UTC)
		k := gpuDayKey{date: day, namespace: r.Namespace, workload: r.WorkloadName, container: r.ContainerName}
		fbMin := int32(math.Round(r.FBUsageMinMiB))
		fbMax := int32(math.Round(r.FBUsageMaxMiB))
		fbAvg := int32(math.Round(r.FBUsageAvgMiB))
		tensorMin := fixedpoint.FloatToBasisPoints(r.TensorPipeMin)
		tensorMax := fixedpoint.FloatToBasisPoints(r.TensorPipeMax)
		tensorAvg := fixedpoint.FloatToBasisPoints(r.TensorPipeAvg)
		dramMin := fixedpoint.FloatToBasisPoints(r.DRAMActiveMin)
		dramMax := fixedpoint.FloatToBasisPoints(r.DRAMActiveMax)
		dramAvg := fixedpoint.FloatToBasisPoints(r.DRAMActiveAvg)
		smMin := fixedpoint.FloatToBasisPoints(r.SMActiveMin)
		smMax := fixedpoint.FloatToBasisPoints(r.SMActiveMax)
		smAvg := fixedpoint.FloatToBasisPoints(r.SMActiveAvg)
		g, ok := groups[k]
		if !ok {
			g = &gpuDayAgg{
				modelName:    r.GPUModel,
				profileName:  r.GPUProfile,
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
			groups[k] = g
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
	for k, g := range groups {
		gpuCount := len(g.gpuUUIDs)
		if gpuCount < 1 {
			gpuCount = 1
		}
		ck := gpu.GPUContainerKey{Namespace: k.namespace, Workload: k.workload, ContainerName: k.container}
		row := gpu.GPUDigestRow{
			IntervalStart:       k.date,
			NodeName:            g.nodeName,
			GPUModelName:        g.modelName,
			GPUProfileName:      g.profileName,
			FBUsageMinMiB:       g.fbMinVal,
			FBUsageMaxMiB:       g.fbMaxVal,
			FBUsageAvgMiB:       gpuMeanInt32(g.fbAvgSum, g.count),
			TensorPipeActiveMin: g.tensorMinVal,
			TensorPipeActiveMax: g.tensorMaxVal,
			TensorPipeActiveAvg: gpuMeanInt32(g.tensorAvgSum, g.count),
			DRAMActiveMin:       g.dramMinVal,
			DRAMActiveMax:       g.dramMaxVal,
			DRAMActiveAvg:       gpuMeanInt32(g.dramAvgSum, g.count),
			SMActiveMin:         g.smMinVal,
			SMActiveMax:         g.smMaxVal,
			SMActiveAvg:         gpuMeanInt32(g.smAvgSum, g.count),
			GPUCount:            gpuCount,
		}
		ds.Grouped[ck] = append(ds.Grouped[ck], row)
		if g.nodeName != "" {
			ds.NodeMap[ck] = g.nodeName
			if prev, ok := ds.NodeLastSeen[g.nodeName]; !ok || k.date.After(prev) {
				ds.NodeLastSeen[g.nodeName] = k.date
			}
		}
	}
	for ck, days := range ds.Grouped {
		slices.SortFunc(days, func(a, b gpu.GPUDigestRow) int {
			if a.IntervalStart.Before(b.IntervalStart) {
				return -1
			}
			if a.IntervalStart.After(b.IntervalStart) {
				return 1
			}
			return 0
		})
		ds.Grouped[ck] = days
	}
	return ds
}

func gpuMeanInt32(sum int64, count int) int32 {
	if count <= 0 {
		return 0
	}
	return int32(sum / int64(count))
}
