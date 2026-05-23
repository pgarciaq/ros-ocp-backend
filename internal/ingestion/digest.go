package ingestion

import (
	"cmp"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
)

// RowWeightFunc returns the schedule weight for a CSV row (0 excludes the sample).
type RowWeightFunc func(MetricRow) float64

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

type weightedPair struct {
	v int64
	w float64
}

const weightedCountingSortMaxSpan = 4096

type weightedDigestScratch struct {
	pairs   []weightedPair
	counts  []int
	sorted  []weightedPair
}

var weightedDigestScratchPool = sync.Pool{
	New: func() any {
		return &weightedDigestScratch{
			pairs: make([]weightedPair, 0, 128),
		}
	},
}

// ComputeWeightedDigest computes percentiles using per-sample weights.
// Samples with weight <= 0 are excluded. When all retained weights are 1.0,
// results match [ComputeDigest] on the same values.
func ComputeWeightedDigest(values []int64, weights []float64) Digest {
	n := len(values)
	if n == 0 || len(weights) != n {
		return Digest{}
	}

	scratch := weightedDigestScratchPool.Get().(*weightedDigestScratch)
	pairs := scratch.pairs[:0]
	if cap(pairs) < n {
		pairs = make([]weightedPair, 0, n)
	}
	for i := range values {
		if weights[i] > 0 {
			pairs = append(pairs, weightedPair{values[i], weights[i]})
		}
	}
	pn := len(pairs)
	if pn == 0 {
		scratch.pairs = pairs
		weightedDigestScratchPool.Put(scratch)
		return Digest{}
	}

	sortWeightedPairs(scratch, pairs)

	var sum int64
	var weightedSum float64
	var totalWeight float64
	allOnes := true
	for _, p := range pairs {
		sum += p.v
		weightedSum += float64(p.v) * p.w
		totalWeight += p.w
		if p.w != 1.0 {
			allOnes = false
		}
	}

	mean := int64(0)
	if totalWeight > 0 {
		mean = int64(weightedSum / totalWeight)
	}

	var p50, p60, p95, p98, p99 int64
	if allOnes {
		p50 = percentileFromWeightedPairs(pairs, 0.50)
		p60 = percentileFromWeightedPairs(pairs, 0.60)
		p95 = percentileFromWeightedPairs(pairs, 0.95)
		p98 = percentileFromWeightedPairs(pairs, 0.98)
		p99 = percentileFromWeightedPairs(pairs, 0.99)
	} else {
		p50, p60, p95, p98, p99 = weightedPercentilesFromPairs(pairs, totalWeight)
	}

	scratch.pairs = pairs
	weightedDigestScratchPool.Put(scratch)

	return Digest{
		P50:   p50,
		P60:   p60,
		P95:   p95,
		P98:   p98,
		P99:   p99,
		Max:   pairs[pn-1].v,
		Mean:  mean,
		Sum:   sum,
		Count: int64(pn),
	}
}

func sortWeightedPairs(scratch *weightedDigestScratch, pairs []weightedPair) {
	pn := len(pairs)
	if pn <= 1 {
		return
	}
	minV, maxV := pairs[0].v, pairs[0].v
	for _, p := range pairs[1:] {
		if p.v < minV {
			minV = p.v
		}
		if p.v > maxV {
			maxV = p.v
		}
	}
	span := int(maxV - minV + 1)
	if span > weightedCountingSortMaxSpan {
		slices.SortFunc(pairs, func(a, b weightedPair) int {
			return cmp.Compare(a.v, b.v)
		})
		return
	}

	counts := scratch.counts
	if cap(counts) < span {
		counts = make([]int, span)
	} else {
		counts = counts[:span]
		clear(counts)
	}
	for _, p := range pairs {
		counts[p.v-minV]++
	}
	pos := 0
	for i := range counts {
		c := counts[i]
		counts[i] = pos
		pos += c
	}

	sorted := scratch.sorted
	if cap(sorted) < pn {
		sorted = make([]weightedPair, pn)
	} else {
		sorted = sorted[:pn]
	}
	for _, p := range pairs {
		idx := counts[p.v-minV]
		sorted[idx] = p
		counts[p.v-minV]++
	}
	copy(pairs, sorted)

	scratch.counts = counts
	scratch.sorted = sorted
}

func percentileFromWeightedPairs(pairs []weightedPair, pct float64) int64 {
	n := len(pairs)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return pairs[0].v
	}
	rank := int(pct * float64(n-1))
	if rank >= n {
		rank = n - 1
	}
	return pairs[rank].v
}

// weightedPercentilesFromPairs returns p50, p60, p95, p98, p99 in one cumulative-weight pass.
func weightedPercentilesFromPairs(pairs []weightedPair, total float64) (p50, p60, p95, p98, p99 int64) {
	if total <= 0 {
		return 0, 0, 0, 0, 0
	}
	n := len(pairs)
	if n == 1 {
		v := pairs[0].v
		return v, v, v, v, v
	}

	targets := [5]float64{
		0.50 * total,
		0.60 * total,
		0.95 * total,
		0.98 * total,
		0.99 * total,
	}
	results := [5]int64{}
	last := pairs[n-1].v
	next := 0
	cum := 0.0
	for i := range pairs {
		cum += pairs[i].w
		for next < 5 && cum >= targets[next] {
			results[next] = pairs[i].v
			next++
		}
	}
	for next < 5 {
		results[next] = last
		next++
	}
	return results[0], results[1], results[2], results[3], results[4]
}

func weightedPercentileFromSorted(sorted []int64, weights []float64, pct float64) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}

	allOnes := true
	for _, w := range weights {
		if w != 1.0 {
			allOnes = false
			break
		}
	}
	if allOnes {
		return percentileFromSorted(sorted, pct)
	}

	var total float64
	for _, w := range weights {
		total += w
	}
	if total <= 0 {
		return 0
	}
	target := pct * total
	cum := 0.0
	for i, w := range weights {
		cum += w
		if cum >= target {
			return sorted[i]
		}
	}
	return sorted[n-1]
}

// GroupCSVRows groups parsed MetricRows by (container, day) for the all_hours stream.
func GroupCSVRows(rows []MetricRow, orgID, clusterUUID string) map[DigestKey][]MetricRow {
	return GroupCSVRowsForStream(rows, orgID, clusterUUID, ScheduleTypeAllHours, nil)
}

// GroupCSVRowsForStream groups rows by container-day and schedule_type.
// When weightFn is non-nil, rows with weight <= 0 are omitted (off_hours_weight=0 fast path).
func GroupCSVRowsForStream(
	rows []MetricRow,
	orgID, clusterUUID string,
	scheduleType ScheduleType,
	weightFn RowWeightFunc,
) map[DigestKey][]MetricRow {
	groups := make(map[DigestKey][]MetricRow, len(rows)/24+1)
	for _, row := range rows {
		if weightFn != nil {
			if w := weightFn(row); w <= 0 {
				continue
			}
		}
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
			ScheduleType:  scheduleType,
		}
		groups[key] = append(groups[key], row)
	}
	return groups
}

// BusinessHoursRowWeightFn builds a weight function for the business_hours stream.
func BusinessHoursRowWeightFn(sched bhschedule.Schedule) RowWeightFunc {
	if !sched.Enabled {
		return nil
	}
	skipZero := sched.OffHoursWeight == 0
	return func(row MetricRow) float64 {
		w := bhschedule.ScheduleWeight(row.IntervalStart, sched)
		if skipZero && w <= 0 {
			return 0
		}
		return w
	}
}

// ComputeContainerDigest computes digest columns for a (container, day, schedule_type) group.
func ComputeContainerDigest(key DigestKey, rows []MetricRow) ContainerDigestResult {
	return ComputeContainerDigestWeighted(key, rows, nil)
}

// ComputeContainerDigestWeighted computes digests with optional per-row weights.
func ComputeContainerDigestWeighted(key DigestKey, rows []MetricRow, weightFn RowWeightFunc) ContainerDigestResult {
	var (
		cpuReqD, cpuUseD, cpuThrD, memReqD, memUseD, memRssD Digest
	)
	if weightFn == nil {
		cpuRequests := extractField(rows, func(r MetricRow) int64 { return r.CPURequestMC })
		cpuUsages := extractField(rows, func(r MetricRow) int64 { return r.CPUUsageMC })
		cpuThrottles := extractField(rows, func(r MetricRow) int64 { return r.CPUThrottleMC })
		memRequests := extractField(rows, func(r MetricRow) int64 { return r.MemRequestKiB })
		memUsages := extractField(rows, func(r MetricRow) int64 { return r.MemUsageKiB })
		memRSS := extractField(rows, func(r MetricRow) int64 { return r.MemRSSKiB })

		cpuReqD = ComputeDigest(cpuRequests)
		cpuUseD = ComputeDigest(cpuUsages)
		cpuThrD = ComputeDigest(cpuThrottles)
		memReqD = ComputeDigest(memRequests)
		memUseD = ComputeDigest(memUsages)
		memRssD = ComputeDigest(memRSS)
	} else {
		cpuReqD, cpuUseD, cpuThrD, memReqD, memUseD, memRssD = computeAllWeightedFieldDigests(rows, weightFn)
	}

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

func computeWeightedFieldDigest(rows []MetricRow, weightFn RowWeightFunc, fieldFn func(MetricRow) int64) Digest {
	vals := make([]int64, 0, len(rows))
	weights := make([]float64, 0, len(rows))
	for _, r := range rows {
		w := weightFn(r)
		if w <= 0 {
			continue
		}
		vals = append(vals, fieldFn(r))
		weights = append(weights, w)
	}
	return ComputeWeightedDigest(vals, weights)
}

type weightedMetricSample struct {
	weight          float64
	cpuReq          int64
	cpuUse          int64
	cpuThr          int64
	memReq          int64
	memUse          int64
	memRss          int64
}

// computeAllWeightedFieldDigests evaluates row weights once and reuses them for all metric fields.
func computeAllWeightedFieldDigests(rows []MetricRow, weightFn RowWeightFunc) (cpuReqD, cpuUseD, cpuThrD, memReqD, memUseD, memRssD Digest) {
	samples := make([]weightedMetricSample, 0, len(rows))
	for _, r := range rows {
		w := weightFn(r)
		if w <= 0 {
			continue
		}
		samples = append(samples, weightedMetricSample{
			weight: w,
			cpuReq: r.CPURequestMC,
			cpuUse: r.CPUUsageMC,
			cpuThr: r.CPUThrottleMC,
			memReq: r.MemRequestKiB,
			memUse: r.MemUsageKiB,
			memRss: r.MemRSSKiB,
		})
	}
	if len(samples) == 0 {
		return Digest{}, Digest{}, Digest{}, Digest{}, Digest{}, Digest{}
	}
	weights := make([]float64, len(samples))
	for i := range samples {
		weights[i] = samples[i].weight
	}
	vals := make([]int64, len(samples))
	for i := range samples {
		vals[i] = samples[i].cpuReq
	}
	cpuReqD = ComputeWeightedDigest(vals, weights)
	for i := range samples {
		vals[i] = samples[i].cpuUse
	}
	cpuUseD = ComputeWeightedDigest(vals, weights)
	for i := range samples {
		vals[i] = samples[i].cpuThr
	}
	cpuThrD = ComputeWeightedDigest(vals, weights)
	for i := range samples {
		vals[i] = samples[i].memReq
	}
	memReqD = ComputeWeightedDigest(vals, weights)
	for i := range samples {
		vals[i] = samples[i].memUse
	}
	memUseD = ComputeWeightedDigest(vals, weights)
	for i := range samples {
		vals[i] = samples[i].memRss
	}
	memRssD = ComputeWeightedDigest(vals, weights)
	return cpuReqD, cpuUseD, cpuThrD, memReqD, memUseD, memRssD
}
