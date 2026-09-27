package types

import (
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/internal/decay"
)

var decayWeightBenchmarkSink float64
var decayPercentileBenchmarkSink int64

func BenchmarkDecayWeight_PerCall(b *testing.B) {
	ages := [...]float64{0, 24, 72, 168, 240, 336, 480, 672}
	DecayWeight(0, 336) // warm the current table before timing
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decayWeightBenchmarkSink = DecayWeight(ages[i%len(ages)], 336)
	}
}

func BenchmarkDecayWeightEvaluator_Resolved(b *testing.B) {
	ages := [...]float64{0, 24, 72, 168, 240, 336, 480, 672}
	evaluator := decay.NewEvaluator(336)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decayWeightBenchmarkSink = evaluator.Weight(ages[i%len(ages)])
	}
}

func BenchmarkMultiWeightedPercentileWithExtras_30Rows(b *testing.B) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := make([]DigestRow, 30)
	for i := range rows {
		rows[i] = DigestRow{
			BucketDate:    now.Add(time.Duration(i-29) * 24 * time.Hour),
			CPUUsageP95MC: int64(i + 1),
		}
	}
	extract := func(row DigestRow) int64 { return row.CPUUsageP95MC }
	var result []int64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, _ = MultiWeightedPercentileWithExtras(rows, now, 336, nil, extract)
	}
	if len(result) != 0 {
		decayPercentileBenchmarkSink = result[0]
	}
}
