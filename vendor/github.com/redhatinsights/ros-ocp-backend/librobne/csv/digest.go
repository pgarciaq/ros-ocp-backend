package csv

import (
	"cmp"
	"math"
	"slices"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/digest"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

type hourKey struct {
	year  int
	month time.Month
	day   int
	hour  int
}

func truncateToHour(t time.Time) hourKey {
	return hourKey{t.Year(), t.Month(), t.Day(), t.Hour()}
}

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

type digestGroupKey struct {
	Namespace     string
	Workload      string
	WorkloadType  string
	ContainerName string
	BucketDate    time.Time
}

// Dataset is parsed ROS container rows plus rate-card lookup metadata.
type Dataset struct {
	Rows      []Row
	Meta      map[types.ContainerKey]RowMeta
	MaxEnd    time.Time
	ClusterID string
}

// DailyDigests groups hourly rows into per-container daily KeyedDigests.
// Output is sorted by container key then BucketDate (RecommendWorkloads order).
func DailyDigests(rows []Row) ([]types.KeyedDigest, Dataset, error) {
	ds := Dataset{
		Rows: rows,
		Meta: make(map[types.ContainerKey]RowMeta, 16),
	}
	if len(rows) == 0 {
		return nil, ds, nil
	}

	groups := make(map[digestGroupKey][]Row, len(rows)/24+1)
	clusters := map[string]struct{}{}
	latestMetaEnd := make(map[types.ContainerKey]time.Time)
	for _, row := range rows {
		end := row.IntervalEnd
		if end.IsZero() {
			end = row.IntervalStart
		}
		if ds.MaxEnd.IsZero() || end.After(ds.MaxEnd) {
			ds.MaxEnd = end
		}
		if row.ClusterID != "" {
			clusters[row.ClusterID] = struct{}{}
		}
		ck := types.ContainerKey{
			Namespace:     row.Namespace,
			Workload:      row.WorkloadName,
			WorkloadType:  row.WorkloadType,
			ContainerName: row.ContainerName,
		}
		if prev, ok := latestMetaEnd[ck]; !ok || end.After(prev) {
			latestMetaEnd[ck] = end
			ds.Meta[ck] = RowMeta{
				InstanceType: row.InstanceType,
				Arch:         row.Arch,
				GPUModel:     row.GPUModel,
				ClusterID:    row.ClusterID,
			}
		}
		bucket := time.Date(
			row.IntervalStart.Year(), row.IntervalStart.Month(), row.IntervalStart.Day(),
			0, 0, 0, 0, time.UTC,
		)
		gk := digestGroupKey{
			Namespace:     row.Namespace,
			Workload:      row.WorkloadName,
			WorkloadType:  row.WorkloadType,
			ContainerName: row.ContainerName,
			BucketDate:    bucket,
		}
		groups[gk] = append(groups[gk], row)
	}
	if len(clusters) == 1 {
		for id := range clusters {
			ds.ClusterID = id
		}
	}

	out := make([]types.KeyedDigest, 0, len(groups))
	for gk, samples := range groups {
		out = append(out, types.KeyedDigest{
			Key: types.ContainerKey{
				Namespace:     gk.Namespace,
				Workload:      gk.Workload,
				WorkloadType:  gk.WorkloadType,
				ContainerName: gk.ContainerName,
			},
			Row: computeDay(gk.BucketDate, samples),
		})
	}
	slices.SortFunc(out, func(a, b types.KeyedDigest) int {
		if c := cmp.Compare(a.Key.Namespace, b.Key.Namespace); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Key.Workload, b.Key.Workload); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Key.WorkloadType, b.Key.WorkloadType); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Key.ContainerName, b.Key.ContainerName); c != 0 {
			return c
		}
		return a.Row.BucketDate.Compare(b.Row.BucketDate)
	})
	return out, ds, nil
}

func computeDay(bucket time.Time, samples []Row) types.DigestRow {
	n := len(samples)
	cpuReq := make([]int64, n)
	cpuUse := make([]int64, n)
	cpuThr := make([]int64, n)
	memReq := make([]int64, n)
	memUse := make([]int64, n)
	memRss := make([]int64, n)
	var oom int64
	for i, s := range samples {
		cpuReq[i] = s.CPURequestMC
		cpuUse[i] = s.CPUUsageMC
		cpuThr[i] = s.CPUThrottleMC
		memReq[i] = s.MemRequestKiB
		memUse[i] = s.MemUsageKiB
		memRss[i] = s.MemRSSKiB
		oom += s.OOMCount
	}
	cpuReqD := digest.ComputeDigest(cpuReq)
	cpuUseD := digest.ComputeDigest(cpuUse)
	cpuThrD := digest.ComputeDigest(cpuThr)
	memReqD := digest.ComputeDigest(memReq)
	memUseD := digest.ComputeDigest(memUse)
	memRssD := digest.ComputeDigest(memRss)
	pcMin, pcMax, pcAvg := computePodCounts(samples)
	desired, available := computeReplicaCounts(samples)
	return types.DigestRow{
		BucketDate:        bucket,
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
		OOMCountSum:       oom,
		CPUUsageMeanMC:    cpuUseD.Mean,
		MemUsageMeanKiB:   memUseD.Mean,
		SampleCount:       cpuUseD.Count,
		PodCountMin:       pcMin,
		PodCountMax:       pcMax,
		PodCountAvg:       pcAvg,
		DesiredReplicas:   desired,
		AvailableReplicas: available,
		CPUUsageCVBP:      computeCPUUsageCVBP(samples),
	}
}

func computePodCounts(samples []Row) (pcMin, pcMax, pcAvg int64) {
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
	var minV, maxV, sum int64
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

func computeReplicaCounts(samples []Row) (desired, available int64) {
	if len(samples) == 0 {
		return 0, 0
	}
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
	latestDesired := latestHour(desiredPerHour)
	latestAvail := latestHour(availPerHour)
	if !latestDesired.zero() {
		desired = desiredPerHour[latestDesired]
	}
	if !latestAvail.zero() {
		available = availPerHour[latestAvail]
	}
	return desired, available
}

func (h hourKey) zero() bool {
	return h.year == 0 && h.month == 0 && h.day == 0 && h.hour == 0
}

func latestHour(m map[hourKey]int64) hourKey {
	var latest hourKey
	first := true
	for h := range m {
		if first {
			latest = h
			first = false
			continue
		}
		if h.isAfter(latest) {
			latest = h
		}
	}
	if first {
		return hourKey{}
	}
	return latest
}

func computeCPUUsageCVBP(samples []Row) *int64 {
	type podHourKey struct {
		hour hourKey
		pod  string
	}
	podHourUsage := make(map[podHourKey]int64)
	hourPods := make(map[hourKey]map[string]struct{})
	for _, s := range samples {
		if s.Pod == "" {
			continue
		}
		h := truncateToHour(s.IntervalStart)
		podHourUsage[podHourKey{hour: h, pod: s.Pod}] += s.CPUUsageMC
		if hourPods[h] == nil {
			hourPods[h] = make(map[string]struct{})
		}
		hourPods[h][s.Pod] = struct{}{}
	}
	if len(hourPods) == 0 {
		return nil
	}
	var cvSum float64
	var cvCount int
	for h, pods := range hourPods {
		if len(pods) < 2 {
			continue
		}
		values := make([]float64, 0, len(pods))
		for pod := range pods {
			values = append(values, float64(podHourUsage[podHourKey{hour: h, pod: pod}]))
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
	if cvCount == 0 {
		return nil
	}
	bp := int64((cvSum / float64(cvCount)) * 10000)
	return &bp
}

type nsDigestGroupKey struct {
	Namespace  string
	BucketDate time.Time
}

// NamespaceDataset is parsed ROS namespace rows plus run metadata.
type NamespaceDataset struct {
	Rows      []NamespaceRow
	MaxEnd    time.Time
	ClusterID string
}

// DailyNamespaceDigests groups hourly namespace rows into per-namespace daily
// DigestRows ordered by BucketDate (RecommendNamespaces window order).
func DailyNamespaceDigests(rows []NamespaceRow) (map[string][]types.DigestRow, NamespaceDataset, error) {
	ds := NamespaceDataset{Rows: rows}
	if len(rows) == 0 {
		return map[string][]types.DigestRow{}, ds, nil
	}

	groups := make(map[nsDigestGroupKey][]NamespaceRow, len(rows)/24+1)
	clusters := map[string]struct{}{}
	for _, row := range rows {
		end := row.IntervalEnd
		if end.IsZero() {
			end = row.IntervalStart
		}
		if ds.MaxEnd.IsZero() || end.After(ds.MaxEnd) {
			ds.MaxEnd = end
		}
		if row.ClusterID != "" {
			clusters[row.ClusterID] = struct{}{}
		}
		bucket := time.Date(
			row.IntervalStart.Year(), row.IntervalStart.Month(), row.IntervalStart.Day(),
			0, 0, 0, 0, time.UTC,
		)
		gk := nsDigestGroupKey{Namespace: row.Namespace, BucketDate: bucket}
		groups[gk] = append(groups[gk], row)
	}
	if len(clusters) == 1 {
		for id := range clusters {
			ds.ClusterID = id
		}
	}

	keyed := make(map[nsDigestGroupKey]types.DigestRow, len(groups))
	for gk, samples := range groups {
		keyed[gk] = computeNamespaceDay(gk.BucketDate, samples)
	}
	out := make(map[string][]types.DigestRow)
	for gk, row := range keyed {
		out[gk.Namespace] = append(out[gk.Namespace], row)
	}
	for ns := range out {
		slices.SortFunc(out[ns], func(a, b types.DigestRow) int {
			return a.BucketDate.Compare(b.BucketDate)
		})
	}
	return out, ds, nil
}

func computeNamespaceDay(bucket time.Time, samples []NamespaceRow) types.DigestRow {
	n := len(samples)
	cpuReq := make([]int64, n)
	cpuUse := make([]int64, n)
	cpuUseMax := make([]int64, n)
	memReq := make([]int64, n)
	memUse := make([]int64, n)
	memUseMax := make([]int64, n)
	for i, s := range samples {
		cpuReq[i] = s.CPURequestMC
		cpuUse[i] = s.CPUUsageMC
		cpuUseMax[i] = s.CPUUsageMaxMC
		memReq[i] = s.MemRequestKiB
		memUse[i] = s.MemUsageKiB
		memUseMax[i] = s.MemUsageMaxKiB
	}
	cpuReqD := digest.ComputeDigest(cpuReq)
	cpuUseD := digest.ComputeDigest(cpuUse)
	memReqD := digest.ComputeDigest(memReq)
	memUseD := digest.ComputeDigest(memUse)
	cpuMax := cpuUseD.Max
	if d := digest.ComputeDigest(cpuUseMax); d.Max > cpuMax {
		cpuMax = d.Max
	}
	memMax := memUseD.Max
	if d := digest.ComputeDigest(memUseMax); d.Max > memMax {
		memMax = d.Max
	}
	return types.DigestRow{
		BucketDate:       bucket,
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
		CPUUsageMaxMC:    cpuMax,
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
		MemUsageMaxKiB:   memMax,
		CPUUsageMeanMC:   cpuUseD.Mean,
		MemUsageMeanKiB:  memUseD.Mean,
		SampleCount:      cpuUseD.Count,
	}
}

// UniqueClusterIDs returns distinct non-empty cluster_id values from rows.
func UniqueClusterIDs(rows []Row) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, row := range rows {
		if row.ClusterID == "" {
			continue
		}
		if _, ok := seen[row.ClusterID]; ok {
			continue
		}
		seen[row.ClusterID] = struct{}{}
		out = append(out, row.ClusterID)
	}
	slices.Sort(out)
	return out
}

// UniqueNamespaceClusterIDs returns distinct non-empty cluster_id values from namespace rows.
func UniqueNamespaceClusterIDs(rows []NamespaceRow) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, row := range rows {
		if row.ClusterID == "" {
			continue
		}
		if _, ok := seen[row.ClusterID]; ok {
			continue
		}
		seen[row.ClusterID] = struct{}{}
		out = append(out, row.ClusterID)
	}
	slices.Sort(out)
	return out
}
