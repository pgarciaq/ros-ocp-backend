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

// SampleWeightFunc returns the schedule weight for a retained digest sample.
type SampleWeightFunc func(metricSample) float64

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
	w int64 // weight scaled by weightScale (10000 = 1.0)
}

const weightScale int64 = 10000

const weightedCountingSortMaxSpan = 4096

const (
	maxCVSpareInner      = 32
	maxWeightedPairsCap  = 512
	resetWeightedPairCap = 128
)

type weightedDigestScratch struct {
	pairs  []weightedPair
	counts []int
	sorted []weightedPair
}

var weightedDigestScratchPool = sync.Pool{
	New: func() any {
		return &weightedDigestScratch{
			pairs: make([]weightedPair, 0, 128),
		}
	},
}

type podHourKey struct {
	hour hourKey
	pod  string
}

type cvScratch struct {
	podHourUsage map[podHourKey]int64
	hourPods     map[hourKey]map[string]struct{}
	spareInner   []map[string]struct{}
	values       []float64
}

func (s *cvScratch) reset() {
	for k := range s.podHourUsage {
		delete(s.podHourUsage, k)
	}
	for k, inner := range s.hourPods {
		for pod := range inner {
			delete(inner, pod)
		}
		s.spareInner = append(s.spareInner, inner)
		delete(s.hourPods, k)
	}
}

func (s *cvScratch) innerMap() map[string]struct{} {
	if n := len(s.spareInner); n > 0 {
		m := s.spareInner[n-1]
		s.spareInner[n-1] = nil
		s.spareInner = s.spareInner[:n-1]
		return m
	}
	return make(map[string]struct{}, 8)
}

var cvScratchPool = sync.Pool{
	New: func() any {
		return &cvScratch{
			podHourUsage: make(map[podHourKey]int64, 64),
			hourPods:     make(map[hourKey]map[string]struct{}, 24),
			spareInner:   make([]map[string]struct{}, 0, 24),
			values:       make([]float64, 0, 8),
		}
	},
}

func capWeightedPairs(scratch *weightedDigestScratch, pairs []weightedPair) []weightedPair {
	if cap(pairs) > maxWeightedPairsCap {
		return make([]weightedPair, 0, resetWeightedPairCap)
	}
	return pairs
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
			scaledW := int64(math.Round(weights[i] * float64(weightScale)))
			if scaledW > 0 {
				pairs = append(pairs, weightedPair{values[i], scaledW})
			}
		}
	}
	pn := len(pairs)
	if pn == 0 {
		scratch.pairs = capWeightedPairs(scratch, pairs)
		weightedDigestScratchPool.Put(scratch)
		return Digest{}
	}

	sortWeightedPairs(scratch, pairs)

	var sum int64
	var weightedSum int64
	var totalWeight int64
	allOnes := true
	for _, p := range pairs {
		sum += p.v
		weightedSum += p.v * p.w
		totalWeight += p.w
		if p.w != weightScale {
			allOnes = false
		}
	}

	mean := int64(0)
	if totalWeight > 0 {
		if allOnes {
			mean = sum / int64(pn)
		} else {
			mean = (weightedSum + totalWeight/2) / totalWeight
		}
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

	scratch.pairs = capWeightedPairs(scratch, pairs)
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
func weightedPercentilesFromPairs(pairs []weightedPair, totalWeight int64) (p50, p60, p95, p98, p99 int64) {
	if totalWeight <= 0 {
		return 0, 0, 0, 0, 0
	}
	n := len(pairs)
	if n == 1 {
		v := pairs[0].v
		return v, v, v, v, v
	}

	targets := [5]int64{
		(50 * totalWeight) / 100,
		(60 * totalWeight) / 100,
		(95 * totalWeight) / 100,
		(98 * totalWeight) / 100,
		(99 * totalWeight) / 100,
	}
	results := [5]int64{}
	last := pairs[n-1].v
	next := 0
	cum := int64(0)
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
func GroupCSVRows(rows []MetricRow, orgID, clusterUUID string) map[DigestKey][]metricSample {
	return GroupCSVRowsForStream(rows, orgID, clusterUUID, ScheduleTypeAllHours, nil)
}

// GroupCSVRowsForStream groups rows by container-day and schedule_type.
// When weightFn is non-nil, rows with weight <= 0 are omitted (off_hours_weight=0 fast path).
func GroupCSVRowsForStream(
	rows []MetricRow,
	orgID, clusterUUID string,
	scheduleType ScheduleType,
	weightFn RowWeightFunc,
) map[DigestKey][]metricSample {
	groups := make(map[DigestKey][]metricSample, len(rows)/24+1)
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
		groups[key] = append(groups[key], metricSampleFromRow(row))
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

// BusinessHoursSampleWeightFn builds a weight function for retained digest samples.
func BusinessHoursSampleWeightFn(sched bhschedule.Schedule) SampleWeightFunc {
	if !sched.Enabled {
		return nil
	}
	skipZero := sched.OffHoursWeight == 0
	return func(s metricSample) float64 {
		w := bhschedule.ScheduleWeight(s.IntervalStart, sched)
		if skipZero && w <= 0 {
			return 0
		}
		return w
	}
}

// ComputeContainerDigest computes digest columns for a (container, day, schedule_type) group.
func ComputeContainerDigest(key DigestKey, rows []MetricRow) ContainerDigestResult {
	return ComputeContainerDigestWeighted(key, metricSamplesFromRows(rows), nil)
}

// ComputeContainerDigestWeighted computes digests with optional per-sample weights.
func ComputeContainerDigestWeighted(key DigestKey, samples []metricSample, weightFn SampleWeightFunc) ContainerDigestResult {
	var (
		cpuReqD, cpuUseD, cpuThrD, memReqD, memUseD, memRssD Digest
	)
	if weightFn == nil {
		cpuReqD, cpuUseD, cpuThrD, memReqD, memUseD, memRssD = computeUnweightedFieldDigests(samples)
	} else {
		cpuReqD, cpuUseD, cpuThrD, memReqD, memUseD, memRssD = computeAllWeightedFieldDigests(samples, weightFn)
	}

	var oomTotal int64
	for _, s := range samples {
		oomTotal += s.OOMCount
	}

	podCountMin, podCountMax, podCountAvg := computePodCounts(samples)
	desiredReplicas, availableReplicas := computeReplicaCounts(samples)
	cpuUsageCVBP := computeCPUUsageCVBP(samples)

	return ContainerDigestResult{
		Key:               key,
		CPURequestP50MC:   cpuReqD.P50,
		CPURequestP60MC:   cpuReqD.P60,
		CPURequestP95MC:   cpuReqD.P95,
		CPURequestP98MC:   cpuReqD.P98,
		CPURequestP99MC:   cpuReqD.P99,
		CPUUsageP50MC:     cpuUseD.P50,
		CPUUsageP60MC:     cpuUseD.P60,
		CPUUsageP95MC:     cpuUseD.P95,
		CPUUsageP98MC:     cpuUseD.P98,
		CPUUsageP99MC:     cpuUseD.P99,
		CPUUsageMaxMC:     cpuUseD.Max,
		CPUThrottleP95MC:  cpuThrD.P95,
		CPUThrottleMaxMC:  cpuThrD.Max,
		MemRequestP50KiB:  memReqD.P50,
		MemRequestP60KiB:  memReqD.P60,
		MemRequestP95KiB:  memReqD.P95,
		MemRequestP98KiB:  memReqD.P98,
		MemRequestP99KiB:  memReqD.P99,
		MemUsageP50KiB:    memUseD.P50,
		MemUsageP60KiB:    memUseD.P60,
		MemUsageP95KiB:    memUseD.P95,
		MemUsageP98KiB:    memUseD.P98,
		MemUsageP99KiB:    memUseD.P99,
		MemUsageMaxKiB:    memUseD.Max,
		MemRSSP95KiB:      memRssD.P95,
		MemRSSMaxKiB:      memRssD.Max,
		OOMCountSum:       oomTotal,
		CPUUsageMeanMC:    cpuUseD.Mean,
		MemUsageMeanKiB:   memUseD.Mean,
		SampleCount:       cpuUseD.Count,
		PodCountMin:       podCountMin,
		PodCountMax:       podCountMax,
		PodCountAvg:       podCountAvg,
		DesiredReplicas:   desiredReplicas,
		AvailableReplicas: availableReplicas,
		CPUUsageCVBP:      cpuUsageCVBP,
	}
}

// ContainerDigestResult holds all computed digest columns for a single
// (container, day) ready for database upsert.
type ContainerDigestResult struct {
	Key               DigestKey
	CPURequestP50MC   int64
	CPURequestP60MC   int64
	CPURequestP95MC   int64
	CPURequestP98MC   int64
	CPURequestP99MC   int64
	CPUUsageP50MC     int64
	CPUUsageP60MC     int64
	CPUUsageP95MC     int64
	CPUUsageP98MC     int64
	CPUUsageP99MC     int64
	CPUUsageMaxMC     int64
	CPUThrottleP95MC  int64
	CPUThrottleMaxMC  int64
	MemRequestP50KiB  int64
	MemRequestP60KiB  int64
	MemRequestP95KiB  int64
	MemRequestP98KiB  int64
	MemRequestP99KiB  int64
	MemUsageP50KiB    int64
	MemUsageP60KiB    int64
	MemUsageP95KiB    int64
	MemUsageP98KiB    int64
	MemUsageP99KiB    int64
	MemUsageMaxKiB    int64
	MemRSSP95KiB      int64
	MemRSSMaxKiB      int64
	OOMCountSum       int64
	CPUUsageMeanMC    int64
	MemUsageMeanKiB   int64
	SampleCount       int64
	PodCountMin       int64
	PodCountMax       int64
	PodCountAvg       int64
	DesiredReplicas   int64
	AvailableReplicas int64
	CPUUsageCVBP      *int64 // coefficient of variation in basis points (0-10000), nil if unavailable
}

// computeCPUUsageCVBP computes the coefficient of variation (CV) of per-pod
// CPU usage across hourly buckets, returned in basis points (0-10000).
// Returns nil if pod identity is unavailable or fewer than 2 pods exist per hour.
//
// All scratch maps and slices are sourced from cvScratchPool to avoid
// ~750k heap allocations per reconcile cycle (30k calls × ~25 maps each).
func computeCPUUsageCVBP(samples []metricSample) *int64 {
	scratch := cvScratchPool.Get().(*cvScratch)
	defer func() {
		scratch.reset()
		if len(scratch.spareInner) > maxCVSpareInner {
			scratch.spareInner = scratch.spareInner[:maxCVSpareInner]
		}
		cvScratchPool.Put(scratch)
	}()

	podHourUsage := scratch.podHourUsage
	hourPods := scratch.hourPods

	for _, s := range samples {
		if s.Pod == "" {
			continue
		}
		h := truncateToHour(s.IntervalStart)
		pk := podHourKey{hour: h, pod: s.Pod}
		podHourUsage[pk] += s.CPUUsageMC
		if hourPods[h] == nil {
			hourPods[h] = scratch.innerMap()
		}
		hourPods[h][s.Pod] = struct{}{}
	}

	if len(hourPods) == 0 {
		return nil
	}

	values := scratch.values
	var cvSum float64
	var cvCount int
	for h, pods := range hourPods {
		if len(pods) < 2 {
			continue
		}
		values = values[:0]
		for pod := range pods {
			pk := podHourKey{hour: h, pod: pod}
			values = append(values, float64(podHourUsage[pk]))
		}
		var sum float64
		for _, v := range values {
			sum += v
		}
		mean := sum / float64(len(values))
		if mean <= 0 {
			continue
		}
		var sqDiffSum float64
		for _, v := range values {
			diff := v - mean
			sqDiffSum += diff * diff
		}
		stddev := math.Sqrt(sqDiffSum / float64(len(values)))
		cvSum += stddev / mean
		cvCount++
	}

	scratch.values = values

	if cvCount == 0 {
		return nil
	}

	avgCV := cvSum / float64(cvCount)
	basisPoints := int64(avgCV * 10000)
	return &basisPoints
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

// isAfter returns true if h is chronologically after other.
func (h hourKey) isAfter(other hourKey) bool {
	if h.year != other.year {
		return h.year > other.year
	}
	if h.month != other.month {
		return h.month > other.month
	}
	if h.day != other.day {
		return h.day > other.day
	}
	return h.hour > other.hour
}

func computePodCounts(samples []metricSample) (pcMin, pcMax, pcAvg int64) {
	if len(samples) == 0 {
		return 0, 0, 0
	}

	hasWPC := false
	for _, s := range samples {
		if s.WorkloadPodCount > 0 {
			hasWPC = true
			break
		}
	}

	if hasWPC {
		maxPerHour := make(map[hourKey]int64)
		for _, s := range samples {
			h := truncateToHour(s.IntervalStart)
			if s.WorkloadPodCount > maxPerHour[h] {
				maxPerHour[h] = s.WorkloadPodCount
			}
		}
		return minMaxAvgOfMap(maxPerHour)
	}

	podsPerHour := make(map[hourKey]map[string]struct{})
	for _, s := range samples {
		if s.Pod == "" {
			continue
		}
		h := truncateToHour(s.IntervalStart)
		if podsPerHour[h] == nil {
			podsPerHour[h] = make(map[string]struct{})
		}
		podsPerHour[h][s.Pod] = struct{}{}
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
	var sum int64
	first := true
	for _, v := range m {
		if first || v < minV {
			minV = v
		}
		if first || v > maxV {
			maxV = v
		}
		sum += v
		first = false
	}
	n := int64(len(m))
	avg := (sum + n/2) / n
	return minV, maxV, avg
}

// computeReplicaCounts returns the most-recent-hour max of desired and
// available replicas. This gives an authoritative snapshot of the replica
// spec state at the end of the digest window. Returns 0 if the column
// was absent (all values zero).
func computeReplicaCounts(samples []metricSample) (desired, available int64) {
	if len(samples) == 0 {
		return 0, 0
	}

	// Group by hour and take max per hour.
	desiredPerHour := make(map[hourKey]int64)
	availPerHour := make(map[hourKey]int64)
	for _, s := range samples {
		h := truncateToHour(s.IntervalStart)
		if s.DesiredReplicas > desiredPerHour[h] {
			desiredPerHour[h] = s.DesiredReplicas
		}
		if s.AvailableReplicas > availPerHour[h] {
			availPerHour[h] = s.AvailableReplicas
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
		if h.isAfter(latestH) {
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
		if h.isAfter(latestH) {
			latestH = h
		}
	}
	if !first {
		available = availPerHour[latestH]
	}

	return desired, available
}

func extractField(samples []metricSample, fn func(metricSample) int64) []int64 {
	buf := fieldBufferPool.Get().([]int64)
	buf = buf[:0]
	if cap(buf) < len(samples) {
		buf = make([]int64, len(samples))
	} else {
		buf = buf[:len(samples)]
	}
	for i, s := range samples {
		buf[i] = fn(s)
	}
	return buf
}

func releaseFieldBuffer(buf []int64) {
	fieldBufferPool.Put(buf[:0])
}

type fieldExtractBuffers struct {
	cpuReq, cpuUse, cpuThr, memReq, memUse, memRss []int64
}

var fieldBufferPool = sync.Pool{
	New: func() any {
		s := make([]int64, 0, 128)
		return s
	},
}

var weightBufferPool = sync.Pool{
	New: func() any {
		s := make([]float64, 0, 128)
		return s
	},
}

var fieldExtractPool = sync.Pool{
	New: func() any {
		return &fieldExtractBuffers{
			cpuReq: make([]int64, 0, 128),
			cpuUse: make([]int64, 0, 128),
			cpuThr: make([]int64, 0, 128),
			memReq: make([]int64, 0, 128),
			memUse: make([]int64, 0, 128),
			memRss: make([]int64, 0, 128),
		}
	},
}

func computeUnweightedFieldDigests(samples []metricSample) (cpuReqD, cpuUseD, cpuThrD, memReqD, memUseD, memRssD Digest) {
	scratch := fieldExtractPool.Get().(*fieldExtractBuffers)
	defer func() {
		scratch.cpuReq = scratch.cpuReq[:0]
		scratch.cpuUse = scratch.cpuUse[:0]
		scratch.cpuThr = scratch.cpuThr[:0]
		scratch.memReq = scratch.memReq[:0]
		scratch.memUse = scratch.memUse[:0]
		scratch.memRss = scratch.memRss[:0]
		fieldExtractPool.Put(scratch)
	}()

	n := len(samples)
	scratch.cpuReq = appendSliceInt64(scratch.cpuReq, n)
	scratch.cpuUse = appendSliceInt64(scratch.cpuUse, n)
	scratch.cpuThr = appendSliceInt64(scratch.cpuThr, n)
	scratch.memReq = appendSliceInt64(scratch.memReq, n)
	scratch.memUse = appendSliceInt64(scratch.memUse, n)
	scratch.memRss = appendSliceInt64(scratch.memRss, n)

	for i, s := range samples {
		scratch.cpuReq[i] = s.CPURequestMC
		scratch.cpuUse[i] = s.CPUUsageMC
		scratch.cpuThr[i] = s.CPUThrottleMC
		scratch.memReq[i] = s.MemRequestKiB
		scratch.memUse[i] = s.MemUsageKiB
		scratch.memRss[i] = s.MemRSSKiB
	}

	cpuReqD = ComputeDigest(scratch.cpuReq)
	cpuUseD = ComputeDigest(scratch.cpuUse)
	cpuThrD = ComputeDigest(scratch.cpuThr)
	memReqD = ComputeDigest(scratch.memReq)
	memUseD = ComputeDigest(scratch.memUse)
	memRssD = ComputeDigest(scratch.memRss)
	return cpuReqD, cpuUseD, cpuThrD, memReqD, memUseD, memRssD
}

func appendSliceInt64(s []int64, n int) []int64 {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]int64, n)
}

func computeWeightedFieldDigest(samples []metricSample, weightFn SampleWeightFunc, fieldFn func(metricSample) int64) Digest {
	vals := fieldBufferPool.Get().([]int64)
	vals = vals[:0]
	weights := weightBufferPool.Get().([]float64)
	weights = weights[:0]

	for _, s := range samples {
		w := weightFn(s)
		if w <= 0 {
			continue
		}
		vals = append(vals, fieldFn(s))
		weights = append(weights, w)
	}
	d := ComputeWeightedDigest(vals, weights)
	fieldBufferPool.Put(vals[:0])
	weightBufferPool.Put(weights[:0])
	return d
}

type weightedMetricSample struct {
	weight float64
	cpuReq int64
	cpuUse int64
	cpuThr int64
	memReq int64
	memUse int64
	memRss int64
}

// computeAllWeightedFieldDigests evaluates sample weights once and reuses them for all metric fields.
func computeAllWeightedFieldDigests(samples []metricSample, weightFn SampleWeightFunc) (cpuReqD, cpuUseD, cpuThrD, memReqD, memUseD, memRssD Digest) {
	weighted := make([]weightedMetricSample, 0, len(samples))
	for _, s := range samples {
		w := weightFn(s)
		if w <= 0 {
			continue
		}
		weighted = append(weighted, weightedMetricSample{
			weight: w,
			cpuReq: s.CPURequestMC,
			cpuUse: s.CPUUsageMC,
			cpuThr: s.CPUThrottleMC,
			memReq: s.MemRequestKiB,
			memUse: s.MemUsageKiB,
			memRss: s.MemRSSKiB,
		})
	}
	if len(weighted) == 0 {
		return Digest{}, Digest{}, Digest{}, Digest{}, Digest{}, Digest{}
	}
	weights := make([]float64, len(weighted))
	for i := range weighted {
		weights[i] = weighted[i].weight
	}
	vals := make([]int64, len(weighted))
	for i := range weighted {
		vals[i] = weighted[i].cpuReq
	}
	cpuReqD = ComputeWeightedDigest(vals, weights)
	for i := range weighted {
		vals[i] = weighted[i].cpuUse
	}
	cpuUseD = ComputeWeightedDigest(vals, weights)
	for i := range weighted {
		vals[i] = weighted[i].cpuThr
	}
	cpuThrD = ComputeWeightedDigest(vals, weights)
	for i := range weighted {
		vals[i] = weighted[i].memReq
	}
	memReqD = ComputeWeightedDigest(vals, weights)
	for i := range weighted {
		vals[i] = weighted[i].memUse
	}
	memUseD = ComputeWeightedDigest(vals, weights)
	for i := range weighted {
		vals[i] = weighted[i].memRss
	}
	memRssD = ComputeWeightedDigest(vals, weights)
	return cpuReqD, cpuUseD, cpuThrD, memReqD, memUseD, memRssD
}
