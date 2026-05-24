package engine

import (
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// BH-PERF-004: dual-stream recommendation overhead <200ms per container.
// Compares all-hours RecommendCPU/Memory vs adding business_hours recommendContainerStream.
//
// Last run (Intel Ultra 7 165H, go test -run=^$ -bench=BenchmarkDualRecommendation -count=3):
//   BH-PERF-004: ~15ms/container dual stream (threshold 200ms) — PASS

func benchDigestRows(days int) []DigestRow {
	rows := make([]DigestRow, days)
	for i := 0; i < days; i++ {
		rows[i] = DigestRow{
			BucketDate:     testutil.BaseDate.AddDate(0, 0, i),
			CPUUsageP95MC:  400 + int64(i*15),
			CPUUsageP60MC:  300 + int64(i*10),
			CPUUsageP50MC:  200 + int64(i*5),
			CPUUsageMeanMC: 250,
			MemUsageP95KiB: 524288,
			MemUsageP60KiB: 400000,
			MemUsageP50KiB: 300000,
			MemUsageMeanKiB: 350000,
			SampleCount:    40,
		}
	}
	return rows
}

func benchMediumTerms() []TermConfig {
	return []TermConfig{
		{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168},
	}
}

func benchmarkAllHoursRec(rows []DigestRow) {
	now := time.Now().UTC()
	cfg := cpuConfigForProfile("cost", now, 168, defaultContainerSizingThresholds)
	_ = RecommendCPU(rows, cfg)
	memCfg := memConfigForProfile("cost", now, 168, defaultContainerSizingThresholds, OOMConfig{})
	_ = RecommendMemory(rows, memCfg)
}

// BenchmarkDualRecommendation_Overhead measures per-container recommendation CPU.
func BenchmarkDualRecommendation_Overhead(b *testing.B) {
	allHours := benchDigestRows(14)
	bhRows := benchDigestRows(14)
	for i := range bhRows {
		bhRows[i].CPUUsageP95MC = 80 + int64(i*5)
		bhRows[i].SampleCount = 35
	}
	terms := benchMediumTerms()
	key := containerKey{
		Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload,
		WorkloadType: testutil.TestWorkloadType, ContainerName: testutil.TestContainer,
	}

	b.Run("all_hours_only", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			benchmarkAllHoursRec(allHours)
		}
	})
	b.Run("all_hours_plus_business_hours", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			benchmarkAllHoursRec(allHours)
			_ = recommendContainerStream(key, bhRows, terms, OOMConfig{}, defaultContainerSizingThresholds)
		}
	})

	// Report per-iteration ms for the dual path (last sub-benchmark timing is approximate).
	b.Run("per_container_ms", func(b *testing.B) {
		b.ResetTimer()
		start := time.Now()
		for i := 0; i < b.N; i++ {
			benchmarkAllHoursRec(allHours)
			_ = recommendContainerStream(key, bhRows, terms, OOMConfig{}, defaultContainerSizingThresholds)
		}
		ms := float64(time.Since(start).Nanoseconds()) / float64(b.N) / 1e6
		b.ReportMetric(ms, "ms/op")
		if ms > 200 {
			b.Logf("BH-PERF-004: %.3fms/container exceeds 200ms threshold", ms)
		}
	})
}
