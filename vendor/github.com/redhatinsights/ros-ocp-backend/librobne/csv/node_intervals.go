package csv

import "time"

// nodeIntervalSum is one interval's totals for a single node: every container
// row that shares an IntervalStart has already been added together.
type nodeIntervalSum struct {
	start  time.Time
	cpuReq int64
	memReq int64
	cpuUse int64
	memUse int64
}

// NodeIntervalSums groups a node-day's container rows by (hour, interval_start).
//
// The ROS CSV carries one row per container per interval, which makes the rows
// inside an hour independent samples of the node's load, not addends of a single
// value. Summing them across the hour multiplies the node's usage and requests
// by the number of intervals the hour holds — four for a 15-minute producer,
// sixty for a per-minute one. Add accumulates per interval instead, and the
// hour-level accessors reduce across the intervals that are actually present:
//
//   - HourUsage is the mean of the interval totals: the hour's average load.
//     Every percentile in the digest is then taken over hourly means, which is
//     what "cpu_usage_p95_mc" estimates — sustained load, not the 4x-inflated
//     sum. For the benchmark fixture the intervals inside an hour differ by
//     ~0.3% (43.44-44.00 cores), so mean and within-hour peak are equivalent
//     there; for a spiky hour the mean is the honest reading of "this hour".
//   - HourRequests is the peak of the interval totals: the reservations the
//     scheduler had to place in that hour. Requests are normally constant
//     across an hour's intervals, so mean and peak agree; they differ only when
//     a pod's request changes mid-hour, and the peak is what sizing needs.
//
// Both node accumulators (internal/ingestion and this package) share this type
// so the two paths cannot drift apart again.
type NodeIntervalSums struct {
	buckets [nodeDayHours][]nodeIntervalSum
}

// Add accumulates one container row observed at start.
func (s *NodeIntervalSums) Add(start time.Time, cpuReq, memReq, cpuUse, memUse int64) {
	h := hourIndex(start)
	bucket := s.buckets[h]
	for i := range bucket {
		if bucket[i].start.Equal(start) {
			bucket[i].cpuReq += cpuReq
			bucket[i].memReq += memReq
			bucket[i].cpuUse += cpuUse
			bucket[i].memUse += memUse
			return
		}
	}
	s.buckets[h] = append(bucket, nodeIntervalSum{
		start:  start,
		cpuReq: cpuReq,
		memReq: memReq,
		cpuUse: cpuUse,
		memUse: memUse,
	})
}

// Seen reports whether hour h holds at least one interval.
func (s *NodeIntervalSums) Seen(h int) bool {
	return h >= 0 && h < nodeDayHours && len(s.buckets[h]) > 0
}

// HourIntervalCount returns how many distinct intervals hour h holds.
func (s *NodeIntervalSums) HourIntervalCount(h int) int {
	if h < 0 || h >= nodeDayHours {
		return 0
	}
	return len(s.buckets[h])
}

// HourUsage returns the mean of hour h's interval totals, rounded to the
// nearest millicore / KiB.
func (s *NodeIntervalSums) HourUsage(h int) (cpuMC, memKiB int64) {
	if h < 0 || h >= nodeDayHours {
		return 0, 0
	}
	bucket := s.buckets[h]
	if len(bucket) == 0 {
		return 0, 0
	}
	var cpu, mem int64
	for i := range bucket {
		cpu += bucket[i].cpuUse
		mem += bucket[i].memUse
	}
	n := int64(len(bucket))
	return meanRounded(cpu, n), meanRounded(mem, n)
}

// HourRequests returns the peak of hour h's interval totals.
func (s *NodeIntervalSums) HourRequests(h int) (cpuMC, memKiB int64) {
	if h < 0 || h >= nodeDayHours {
		return 0, 0
	}
	bucket := s.buckets[h]
	for i := range bucket {
		if bucket[i].cpuReq > cpuMC {
			cpuMC = bucket[i].cpuReq
		}
		if bucket[i].memReq > memKiB {
			memKiB = bucket[i].memReq
		}
	}
	return cpuMC, memKiB
}

// meanRounded divides sum by n without truncating toward zero.
func meanRounded(sum, n int64) int64 {
	if n <= 1 {
		return sum
	}
	if sum >= 0 {
		return (sum + n/2) / n
	}
	return -((-sum + n/2) / n)
}
