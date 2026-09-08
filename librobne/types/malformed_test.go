package types

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Concurrent Set/Report must be race-free: meaningful under -race (the full
// suite runs RACE=1), trivially green otherwise. Pre-atomic code fails this
// under the detector with a read/write race on the hook var (#553).
func TestMalformedJSONReporter_ConcurrentSetReport(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				SetMalformedJSONReporter(func(string) {})
				ReportMalformedJSON(SiteSnapshotLabels)
			}
		}()
	}
	wg.Wait()
	SetMalformedJSONReporter(nil)

	// Nil reporter stays a no-op (must not panic after the churn above).
	require.NotPanics(t, func() { ReportMalformedJSON(SiteSnapshotLabels) })
}
