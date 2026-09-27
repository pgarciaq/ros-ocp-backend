package node

import "testing"

var nodeClassificationBenchmarkSink nodeClassification

func BenchmarkClassifyNode_30Days(b *testing.B) {
	allocCPU, allocMem := ptr64(16000), ptr64(65536)
	days := make([]DigestRow, 30)
	for i := range days {
		day := i + 1
		days[i] = makeDigestRow("bench-node", day,
			8000+int64(day), 10000+int64(day), 30000+int64(day), 40000+int64(day),
			12000, 48000, allocCPU, allocMem)
	}
	endDate := days[len(days)-1].BucketDate
	cfg := defaultRecConfig()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodeClassificationBenchmarkSink = classifyNode("bench-node", days, cfg, defaultThresholdSettings, 336, endDate)
	}
}
