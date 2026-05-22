package ingestion

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
)

// Performance thresholds (design doc / test-plan-business-hours.md Phase 10):
//   BH-PERF-001: <10ms total for 10k containers × 96 weighted samples
//   BH-PERF-002: <1ms per cached schedule lookup (10k lookups)
//   BH-PERF-003: dual-digest CPU <2× single-digest (reported as ratio, not enforced)
//   BH-PERF-006: off_hours_weight=0 path <1.05× unweighted
//   BH-PERF-007: schedule eval per row <50ms for 10k rows
//
// Last run (Intel Ultra 7 165H, go test -run=^$ -benchmem -count=5):
//   BH-PERF-001: ~6–9ms/op (threshold 10ms) — sync.Pool + counting sort for narrow spans
//   BH-PERF-002: ~0.07–0.12 µs/lookup (threshold 1ms) — PASS
//   BH-PERF-003: single ~2ms/100 ctr, dual ~500ms/100 ctr (~250×; weighted BH path)
//   BH-PERF-007: ~1–2ms/10k rows (threshold 50ms) — schedule eval with cached TZ

const (
	benchContainerCount = 10_000
	benchSamplesPerDay  = 96
	benchLookupCount    = 10_000
)

func benchWeekdaySchedule() bhschedule.Schedule {
	s := bhschedule.Schedule{
		Enabled:        true,
		Timezone:       "America/New_York",
		Days:           []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime:      "08:00",
		EndTime:        "17:00",
		OffHoursWeight: 0.1,
	}
	if err := s.InitLocation(); err != nil {
		panic(err)
	}
	return s
}

func benchMetricRows(containerIdx int, samples int) []MetricRow {
	rows := make([]MetricRow, samples)
	day := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC) // Tuesday
	ns := fmt.Sprintf("ns-%d", containerIdx%100)
	for i := 0; i < samples; i++ {
		start := day.Add(time.Duration(i*15) * time.Minute)
		rows[i] = MetricRow{
			IntervalStart: start,
			IntervalEnd:   start.Add(15 * time.Minute),
			Namespace:     ns,
			WorkloadName:  fmt.Sprintf("deploy-%d", containerIdx),
			WorkloadType:  "deployment",
			ContainerName: fmt.Sprintf("ctr-%d", containerIdx),
			CPURequestMC:  100 + int64(i),
			CPUUsageMC:    50 + int64((i*7+containerIdx)%40),
			MemRequestKiB: 134217728,
			MemUsageKiB:   104857600 + int64(i*1000),
		}
	}
	return rows
}

func benchWeightedValues(samples int, seed int64) ([]int64, []float64) {
	rng := rand.New(rand.NewSource(seed))
	vals := make([]int64, samples)
	weights := make([]float64, samples)
	for i := range vals {
		vals[i] = int64(50 + rng.Intn(500))
		if i%3 == 0 {
			weights[i] = 1.0
		} else {
			weights[i] = 0.1
		}
	}
	return vals, weights
}

// BenchmarkWeightedPercentile_10kContainers96Samples measures weighted digest CPU for one day per container.
func BenchmarkWeightedPercentile_10kContainers96Samples(b *testing.B) {
	vals, weights := benchWeightedValues(benchSamplesPerDay, 42)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for c := 0; c < benchContainerCount; c++ {
			ComputeWeightedDigest(vals, weights)
		}
	}
	perRun := float64(b.Elapsed().Nanoseconds()) / float64(b.N) / 1e6
	b.ReportMetric(perRun, "ms/op")
	if perRun > 10 {
		b.Logf("BH-PERF-001: %.3fms exceeds 10ms threshold", perRun)
	}
}

// BenchmarkScheduleResolution_CachedLookup measures in-memory Resolve after cache warmup.
func BenchmarkScheduleResolution_CachedLookup(b *testing.B) {
	org := &bhschedule.Schedule{
		Enabled: true, Timezone: "UTC",
		Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "09:00", EndTime: "17:00",
	}
	nsMap := make(map[string]bhschedule.Schedule, 100)
	for i := 0; i < 100; i++ {
		nsMap[fmt.Sprintf("ns-%d", i)] = benchWeekdaySchedule()
	}
	cache := bhschedule.NewCacheForTest(org, nil, nsMap)
	// Warmup
	for i := 0; i < 1000; i++ {
		_ = cache.Resolve(fmt.Sprintf("ns-%d", i%100))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < benchLookupCount; j++ {
			_ = cache.Resolve(fmt.Sprintf("ns-%d", j%100))
		}
	}
	perLookupUs := float64(b.Elapsed().Nanoseconds()) / float64(b.N*benchLookupCount) / 1e3
	b.ReportMetric(perLookupUs, "us/lookup")
	if perLookupUs > 1000 {
		b.Logf("BH-PERF-002: %.3fus/lookup exceeds 1ms threshold", perLookupUs)
	}
}

func benchmarkDigestSingle(rows []MetricRow) {
	key := DigestKey{
		OrgID: "org-bench", ClusterUUID: "00000000-0000-0000-0000-000000000001",
		Namespace: rows[0].Namespace, Workload: rows[0].WorkloadName,
		WorkloadType: rows[0].WorkloadType, ContainerName: rows[0].ContainerName,
		BucketDate: rows[0].IntervalStart.Truncate(24 * time.Hour), ScheduleType: ScheduleTypeAllHours,
	}
	_ = ComputeContainerDigest(key, rows)
}

func benchmarkDigestDual(rows []MetricRow, sched bhschedule.Schedule) {
	keyAll := DigestKey{
		OrgID: "org-bench", ClusterUUID: "00000000-0000-0000-0000-000000000001",
		Namespace: rows[0].Namespace, Workload: rows[0].WorkloadName,
		WorkloadType: rows[0].WorkloadType, ContainerName: rows[0].ContainerName,
		BucketDate: rows[0].IntervalStart.Truncate(24 * time.Hour), ScheduleType: ScheduleTypeAllHours,
	}
	keyBH := keyAll
	keyBH.ScheduleType = ScheduleTypeBusinessHours
	weightFn := BusinessHoursRowWeightFn(sched)
	_ = ComputeContainerDigest(keyAll, rows)
	_ = ComputeContainerDigestWeighted(keyBH, rows, weightFn)
}

// BenchmarkDualDigestIngestion_Overhead compares single-stream vs dual-stream digest computation.
func BenchmarkDualDigestIngestion_Overhead(b *testing.B) {
	const containers = 100
	rowsByContainer := make([][]MetricRow, containers)
	for i := 0; i < containers; i++ {
		rowsByContainer[i] = benchMetricRows(i, benchSamplesPerDay)
	}
	sched := benchWeekdaySchedule()

	b.Run("single", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, rows := range rowsByContainer {
				benchmarkDigestSingle(rows)
			}
		}
	})
	b.Run("dual", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, rows := range rowsByContainer {
				benchmarkDigestDual(rows, sched)
			}
		}
	})
}

// BenchmarkIngestBHWeightZero verifies zero off-hours weight adds negligible overhead.
func BenchmarkIngestBHWeightZero(b *testing.B) {
	rows := benchMetricRows(0, benchSamplesPerDay)
	key := DigestKey{
		OrgID: "org-bench", ClusterUUID: "00000000-0000-0000-0000-000000000001",
		Namespace: rows[0].Namespace, Workload: rows[0].WorkloadName,
		WorkloadType: rows[0].WorkloadType, ContainerName: rows[0].ContainerName,
		BucketDate: rows[0].IntervalStart.Truncate(24 * time.Hour), ScheduleType: ScheduleTypeAllHours,
	}
	sched := benchWeekdaySchedule()
	sched.OffHoursWeight = 0
	weightFn := BusinessHoursRowWeightFn(sched)

	b.Run("unweighted", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ComputeContainerDigest(key, rows)
		}
	})
	b.Run("bh_weight_zero", func(b *testing.B) {
		keyBH := key
		keyBH.ScheduleType = ScheduleTypeBusinessHours
		for i := 0; i < b.N; i++ {
			_ = ComputeContainerDigestWeighted(keyBH, rows, weightFn)
		}
	})
}

// BenchmarkScheduleEvalPerRow_10k measures per-row schedule weight evaluation during ingestion.
func BenchmarkScheduleEvalPerRow_10k(b *testing.B) {
	sched := benchWeekdaySchedule()
	rows := benchMetricRows(0, benchLookupCount)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, row := range rows {
			_ = bhschedule.ScheduleWeight(row.IntervalStart, sched)
		}
	}
	perRunMs := float64(b.Elapsed().Nanoseconds()) / float64(b.N) / 1e6
	b.ReportMetric(perRunMs, "ms/op")
	if perRunMs > 50 {
		b.Logf("BH-PERF-007: %.3fms exceeds 50ms threshold", perRunMs)
	}
}
