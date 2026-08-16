package csv

import (
	"cmp"
	"slices"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
)

const pvcHourlyIntervalSeconds int64 = 3600

type pvcDigestKey struct {
	Date      time.Time
	Namespace string
	PVC       string
}

// PVCDataset is daily PVC aggregation metadata (MaxEnd is the engine clock fallback).
type PVCDataset struct {
	MaxEnd time.Time
}

// DailyPVCDigests aggregates storage CSV rows into per-PVC daily digests.
// Byte-seconds convert to bytes the same way as ingest ComputePVCDigests.
func DailyPVCDigests(rows []PVCRow) (map[pvc.PVCKey][]pvc.PVCDigestRow, PVCDataset) {
	var ds PVCDataset
	if len(rows) == 0 {
		return map[pvc.PVCKey][]pvc.PVCDigestRow{}, ds
	}

	type accumulator struct {
		pv             string
		storageClass   string
		capacity       int64
		request        int64
		usageSum       int64
		usageMin       int64
		usageMax       int64
		count          int
		lastSeenPod    string
		vmName         string
		latestInterval time.Time
	}

	groups := make(map[pvcDigestKey]*accumulator)
	for _, r := range rows {
		end := r.IntervalEnd
		if end.IsZero() {
			end = r.IntervalStart
		}
		if ds.MaxEnd.IsZero() || end.After(ds.MaxEnd) {
			ds.MaxEnd = end
		}
		date := time.Date(r.IntervalStart.Year(), r.IntervalStart.Month(), r.IntervalStart.Day(), 0, 0, 0, 0, time.UTC)
		key := pvcDigestKey{Date: date, Namespace: r.Namespace, PVC: r.PersistentVolumeClaim}

		intervalSeconds := int64(r.IntervalEnd.Sub(r.IntervalStart).Seconds())
		if intervalSeconds <= 0 {
			intervalSeconds = pvcHourlyIntervalSeconds
		}

		capacityBytes := r.CapacityBytes
		if r.UsageByteSeconds > 0 && capacityBytes > 1e12 {
			capacityBytes = (capacityBytes + intervalSeconds/2) / intervalSeconds
		}
		usageBytes := (r.UsageByteSeconds + pvcHourlyIntervalSeconds/2) / pvcHourlyIntervalSeconds
		requestBytes := (r.RequestByteSeconds + pvcHourlyIntervalSeconds/2) / pvcHourlyIntervalSeconds

		acc, ok := groups[key]
		if !ok {
			acc = &accumulator{
				pv:           r.PersistentVolume,
				storageClass: r.StorageClass,
				capacity:     capacityBytes,
				request:      requestBytes,
				usageMin:     usageBytes,
				usageMax:     usageBytes,
			}
			groups[key] = acc
		}

		if capacityBytes > acc.capacity {
			acc.capacity = capacityBytes
		}
		if requestBytes > acc.request {
			acc.request = requestBytes
		}
		if usageBytes < acc.usageMin {
			acc.usageMin = usageBytes
		}
		if usageBytes > acc.usageMax {
			acc.usageMax = usageBytes
		}
		acc.usageSum += usageBytes
		acc.count++
		if r.PersistentVolume != "" {
			acc.pv = r.PersistentVolume
		}
		if r.StorageClass != "" {
			acc.storageClass = r.StorageClass
		}
		if r.Pod != "" && (acc.latestInterval.IsZero() || !r.IntervalEnd.Before(acc.latestInterval)) {
			acc.latestInterval = r.IntervalEnd
			acc.lastSeenPod = r.Pod
			if r.VMName != "" {
				acc.vmName = r.VMName
			}
		}
		if r.VMName != "" && acc.vmName == "" {
			acc.vmName = r.VMName
		}
	}

	out := make(map[pvc.PVCKey][]pvc.PVCDigestRow)
	for key, acc := range groups {
		avg := acc.usageSum
		if acc.count > 0 {
			avg = acc.usageSum / int64(acc.count)
		}
		pk := pvc.PVCKey{Namespace: key.Namespace, PVC: key.PVC}
		out[pk] = append(out[pk], pvc.PVCDigestRow{
			BucketDate:    key.Date,
			Namespace:     key.Namespace,
			PVC:           key.PVC,
			LastSeenPod:   acc.lastSeenPod,
			VMName:        acc.vmName,
			PV:            acc.pv,
			StorageClass:  acc.storageClass,
			CapacityBytes: acc.capacity,
			RequestBytes:  acc.request,
			UsageBytesMin: acc.usageMin,
			UsageBytesMax: acc.usageMax,
			UsageBytesAvg: avg,
			SampleCount:   acc.count,
		})
	}
	for k, days := range out {
		slices.SortFunc(days, func(a, b pvc.PVCDigestRow) int {
			return cmp.Compare(a.BucketDate.Unix(), b.BucketDate.Unix())
		})
		out[k] = days
	}
	return out, ds
}
