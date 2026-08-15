package engine

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkRecommendWorkloads_ComputeOnly is the P3 extract canary: synthetic
// KeyedDigest rows, emit discarded, no pool. Compare ns/op and allocs/op with
// benchstat against docs/performance/librobne-baseline-841639f3/compute-only-bench.txt
// (first recorded at P3). Do not duplicate the runner in this file.
func BenchmarkRecommendWorkloads_ComputeOnly(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
			rows := syntheticKeyedDigests(n, 14)
			now := rows[len(rows)-1].Row.BucketDate
			cfg := DefaultEngineConfig("org-bench", "cluster-bench", now)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := RecommendWorkloads(ctx, rows, cfg, discardContainerRecs); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func discardContainerRecs([]ContainerRec) error { return nil }
