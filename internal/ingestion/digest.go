package ingestion

import (
	"math"
	"slices"
	"time"
)

// ComputeDigest computes exact percentiles and aggregates from a sorted slice
// of int64 values. Uses slices.Sort() for O(n log n) exact computation.
// The input slice is sorted in place.
func ComputeDigest(values []int64) Digest {
	n := len(values)
	if n == 0 {
		return Digest{}
	}

	slices.Sort(values)

	var sum int64
	for _, v := range values {
		sum += v
	}

	return Digest{
		P50:   percentileFromSorted(values, 0.50),
		P60:   percentileFromSorted(values, 0.60),
		P95:   percentileFromSorted(values, 0.95),
		P98:   percentileFromSorted(values, 0.98),
		P99:   percentileFromSorted(values, 0.99),
		Max:   values[n-1],
		Mean:  sum / int64(n),
		Sum:   sum,
		Count: int64(n),
	}
}

// percentileFromSorted returns the value at the given percentile from a
// pre-sorted slice using the nearest-lower-rank method: index = floor(pct * (n-1)).
func percentileFromSorted(sorted []int64, pct float64) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	idx := int(pct * float64(n-1))
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

// GroupCSVRows groups parsed MetricRows by (container, day) for digest
// computation. The orgID and clusterUUID are provided by the caller
// (from the Kafka message metadata).
func GroupCSVRows(rows []MetricRow, orgID, clusterUUID string) map[DigestKey][]MetricRow {
	groups := make(map[DigestKey][]MetricRow)
	for _, row := range rows {
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
		}
		groups[key] = append(groups[key], row)
	}
	return groups
}

// ComputeContainerDigest computes a full set of digest columns for a group
// of MetricRows belonging to the same (container, day).
func ComputeContainerDigest(key DigestKey, rows []MetricRow) ContainerDigestResult {
	cpuRequests := extractField(rows, func(r MetricRow) int64 { return r.CPURequestMC })
	cpuUsages := extractField(rows, func(r MetricRow) int64 { return r.CPUUsageMC })
	cpuThrottles := extractField(rows, func(r MetricRow) int64 { return r.CPUThrottleMC })
	memRequests := extractField(rows, func(r MetricRow) int64 { return r.MemRequestKiB })
	memUsages := extractField(rows, func(r MetricRow) int64 { return r.MemUsageKiB })
	memRSS := extractField(rows, func(r MetricRow) int64 { return r.MemRSSKiB })

	cpuReqD := ComputeDigest(cpuRequests)
	cpuUseD := ComputeDigest(cpuUsages)
	cpuThrD := ComputeDigest(cpuThrottles)
	memReqD := ComputeDigest(memRequests)
	memUseD := ComputeDigest(memUsages)
	memRssD := ComputeDigest(memRSS)

	var oomTotal int64
	for _, r := range rows {
		oomTotal += r.OOMCount
	}

	podCountMin, podCountMax, podCountAvg := computePodCounts(rows)
	desiredReplicas, availableReplicas := computeReplicaCounts(rows)

	return ContainerDigestResult{
		Key:              key,
		CPURequestP50MC:  cpuReqD.P50,
		CPURequestP60MC:  cpuReqD.P60,
		CPURequestP95MC:  cpuReqD.P95,
		CPURequestP98MC:  cpuReqD.P98,
		CPURequestP99MC:  cpuReqD.P99,
		CPUUsageP50MC:    cpuUseD.P50,
		CPUUsageP60MC:    cpuUseD.P60,
		CPUUsageP95MC:    cpuUseD.P95,
		CPUUsageP98MC:    cpuUseD.P98,
		CPUUsageP99MC:    cpuUseD.P99,
		CPUUsageMaxMC:    cpuUseD.Max,
		CPUThrottleP95MC: cpuThrD.P95,
		CPUThrottleMaxMC: cpuThrD.Max,
		MemRequestP50KiB: memReqD.P50,
		MemRequestP60KiB: memReqD.P60,
		MemRequestP95KiB: memReqD.P95,
		MemRequestP98KiB: memReqD.P98,
		MemRequestP99KiB: memReqD.P99,
		MemUsageP50KiB:   memUseD.P50,
		MemUsageP60KiB:   memUseD.P60,
		MemUsageP95KiB:   memUseD.P95,
		MemUsageP98KiB:   memUseD.P98,
		MemUsageP99KiB:   memUseD.P99,
		MemUsageMaxKiB:   memUseD.Max,
		MemRSSP95KiB:     memRssD.P95,
		MemRSSMaxKiB:     memRssD.Max,
		OOMCountSum:      oomTotal,
		CPUUsageMeanMC:   cpuUseD.Mean,
		MemUsageMeanKiB:  memUseD.Mean,
		SampleCount:      cpuUseD.Count,
		PodCountMin:       podCountMin,
		PodCountMax:       podCountMax,
		PodCountAvg:       podCountAvg,
		DesiredReplicas:   desiredReplicas,
		AvailableReplicas: availableReplicas,
	}
}

// ContainerDigestResult holds all computed digest columns for a single
// (container, day) ready for database upsert.
type ContainerDigestResult struct {
	Key              DigestKey
	CPURequestP50MC  int64
	CPURequestP60MC  int64
	CPURequestP95MC  int64
	CPURequestP98MC  int64
	CPURequestP99MC  int64
	CPUUsageP50MC    int64
	CPUUsageP60MC    int64
	CPUUsageP95MC    int64
	CPUUsageP98MC    int64
	CPUUsageP99MC    int64
	CPUUsageMaxMC    int64
	CPUThrottleP95MC int64
	CPUThrottleMaxMC int64
	MemRequestP50KiB int64
	MemRequestP60KiB int64
	MemRequestP95KiB int64
	MemRequestP98KiB int64
	MemRequestP99KiB int64
	MemUsageP50KiB   int64
	MemUsageP60KiB   int64
	MemUsageP95KiB   int64
	MemUsageP98KiB   int64
	MemUsageP99KiB   int64
	MemUsageMaxKiB   int64
	MemRSSP95KiB     int64
	MemRSSMaxKiB     int64
	OOMCountSum      int64
	CPUUsageMeanMC   int64
	MemUsageMeanKiB  int64
	SampleCount      int64
	PodCountMin       int64
	PodCountMax       int64
	PodCountAvg       int64
	DesiredReplicas   int64
	AvailableReplicas int64
}

// computePodCounts derives per-day pod count min/max/avg from hourly buckets.
//
// Primary strategy: if any row has WorkloadPodCount > 0 (new operator), group
// rows by IntervalStart hour and take the max WorkloadPodCount per bucket
// (all pods in a workload report the same count; max is defensive).
//
// Fallback strategy: if all WorkloadPodCount values are 0 (old operator),
// count distinct Pod names per hourly bucket.
//
// Then compute min/max/avg of the hourly counts across the day.
type hourKey struct {
	year  int
	month time.Month
	day   int
	hour  int
}

func truncateToHour(t time.Time) hourKey {
	return hourKey{t.Year(), t.Month(), t.Day(), t.Hour()}
}

func computePodCounts(rows []MetricRow) (pcMin, pcMax, pcAvg int64) {
	if len(rows) == 0 {
		return 0, 0, 0
	}

	hasWPC := false
	for _, r := range rows {
		if r.WorkloadPodCount > 0 {
			hasWPC = true
			break
		}
	}

	if hasWPC {
		maxPerHour := make(map[hourKey]int64)
		for _, r := range rows {
			h := truncateToHour(r.IntervalStart)
			if r.WorkloadPodCount > maxPerHour[h] {
				maxPerHour[h] = r.WorkloadPodCount
			}
		}
		return minMaxAvgOfMap(maxPerHour)
	}

	podsPerHour := make(map[hourKey]map[string]struct{})
	for _, r := range rows {
		if r.Pod == "" {
			continue
		}
		h := truncateToHour(r.IntervalStart)
		if podsPerHour[h] == nil {
			podsPerHour[h] = make(map[string]struct{})
		}
		podsPerHour[h][r.Pod] = struct{}{}
	}
	countPerHour := make(map[hourKey]int64, len(podsPerHour))
	for h, pods := range podsPerHour {
		countPerHour[h] = int64(len(pods))
	}
	return minMaxAvgOfMap(countPerHour)
}

func minMaxAvgOfMap(m map[hourKey]int64) (int64, int64, int64) {
	if len(m) == 0 {
		return 0, 0, 0
	}
	var minV, maxV int64
	var sum float64
	first := true
	for _, v := range m {
		if first || v < minV {
			minV = v
		}
		if first || v > maxV {
			maxV = v
		}
		sum += float64(v)
		first = false
	}
	avg := int64(math.Round(sum / float64(len(m))))
	return minV, maxV, avg
}

// computeReplicaCounts returns the most-recent-hour max of desired and
// available replicas. This gives an authoritative snapshot of the replica
// spec state at the end of the digest window. Returns 0 if the column
// was absent (all values zero).
func computeReplicaCounts(rows []MetricRow) (desired, available int64) {
	if len(rows) == 0 {
		return 0, 0
	}

	// Group by hour and take max per hour.
	desiredPerHour := make(map[hourKey]int64)
	availPerHour := make(map[hourKey]int64)
	for _, r := range rows {
		h := truncateToHour(r.IntervalStart)
		if r.DesiredReplicas > desiredPerHour[h] {
			desiredPerHour[h] = r.DesiredReplicas
		}
		if r.AvailableReplicas > availPerHour[h] {
			availPerHour[h] = r.AvailableReplicas
		}
	}

	// Take the latest hour's value.
	var latestH hourKey
	first := true
	for h := range desiredPerHour {
		if first {
			latestH = h
			first = false
			continue
		}
		hTime := time.Date(h.year, h.month, h.day, h.hour, 0, 0, 0, time.UTC)
		latestTime := time.Date(latestH.year, latestH.month, latestH.day, latestH.hour, 0, 0, 0, time.UTC)
		if hTime.After(latestTime) {
			latestH = h
		}
	}
	if !first {
		desired = desiredPerHour[latestH]
	}

	// Repeat for available (might have different hours present).
	first = true
	for h := range availPerHour {
		if first {
			latestH = h
			first = false
			continue
		}
		hTime := time.Date(h.year, h.month, h.day, h.hour, 0, 0, 0, time.UTC)
		latestTime := time.Date(latestH.year, latestH.month, latestH.day, latestH.hour, 0, 0, 0, time.UTC)
		if hTime.After(latestTime) {
			latestH = h
		}
	}
	if !first {
		available = availPerHour[latestH]
	}

	return desired, available
}

func extractField(rows []MetricRow, fn func(MetricRow) int64) []int64 {
	vals := make([]int64, len(rows))
	for i, r := range rows {
		vals[i] = fn(r)
	}
	return vals
}
