package pvc

import "testing"

var pvcGrowthSlopeBenchmarkSink float64

func BenchmarkComputePVCGrowthSlopeWLS_30Days(b *testing.B) {
	digests := make([]PVCDigestRow, 30)
	for i := range digests {
		digests[i].UsageBytesAvg = int64(i+1) * (1 << 20)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pvcGrowthSlopeBenchmarkSink = computePVCGrowthSlopeWLS(digests, 336)
	}
}
