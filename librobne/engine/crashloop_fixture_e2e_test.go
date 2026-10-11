package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// TestCrashLoopFixtureEndToEnd runs the committed synthetic crash-loop package
// (testdata/crashloop/crash-fixture.tar.gz) through the real file path:
// tarball load (restart_count parse) → daily digests (RestartCountSum) →
// engine recommend (cost-downsize gate + notification codes).
//
// Fixture: workload crash-a (400mc/1GiB requests, 5 restarts on one last-day
// row), clean-b (200mc/512MiB requests, zero restarts), oom-c (400mc/1GiB
// requests, 5 OOM + 5 restarts on last-day rows). The three workloads carry
// distinct request values so incidental equality cannot green the assertions.
func TestCrashLoopFixtureEndToEnd(t *testing.T) {
	loaded, err := csv.Load("testdata/crashloop/crash-fixture.tar.gz")
	require.NoError(t, err)
	require.Len(t, loaded.Rows, 168, "3 workloads × 14 days × 4 intervals")
	require.NotNil(t, loaded.Manifest, "package must mirror the operator tarball layout")

	digests, _, err := csv.DailyDigests(loaded.Rows)
	require.NoError(t, err)
	require.Len(t, digests, 42, "3 workloads × 14 daily buckets")

	now := time.Time{}
	for _, d := range digests {
		if d.Row.BucketDate.After(now) {
			now = d.Row.BucketDate
		}
	}
	require.False(t, now.IsZero())

	var recs []ContainerRec
	err = RecommendWorkloads(context.Background(), digests, DefaultEngineConfig("org", "cluster", now),
		func(batch []ContainerRec) error {
			recs = append(recs, batch...)
			return nil
		})
	require.NoError(t, err)
	require.Len(t, recs, 18, "3 workloads × 3 terms × 2 engines")

	byKey := make(map[string]ContainerRec, len(recs))
	for _, r := range recs {
		byKey[r.Workload+"/"+r.Term+"/"+r.Engine] = r
	}

	for _, term := range []string{"short", "medium", "long"} {
		// A: crash-looping with a downsize signal — cost rec pinned at
		// current with zeroed savings, UNSTABLE_CONTAINER attached, no OOM code.
		a := byKey["crash-a/"+term+"/cost"]
		require.Equal(t, "cost", a.Engine)
		assert.Equal(t, a.CurrentCPURequestMC, a.RecCPURequestMC, "term %s cost CPU held", term)
		assert.Equal(t, a.CurrentMemRequestKiB, a.RecMemRequestKiB, "term %s cost mem held", term)
		require.NotNil(t, a.EstimatedSavingsCents, "term %s savings zeroed, not nil", term)
		assert.Equal(t, int64(0), *a.EstimatedSavingsCents, "term %s", term)
		assert.Contains(t, a.NotificationCodes, types.NotifUnstableContainer, "term %s", term)
		assert.NotContains(t, a.NotificationCodes, types.NotifOOMDetected, "term %s", term)

		// B: clean with a downsize signal — downsizes normally, no crash code.
		b := byKey["clean-b/"+term+"/cost"]
		assert.Less(t, b.RecCPURequestMC, b.CurrentCPURequestMC, "term %s must downsize CPU", term)
		assert.Less(t, b.RecMemRequestKiB, b.CurrentMemRequestKiB, "term %s must downsize mem", term)
		assert.NotContains(t, b.NotificationCodes, types.NotifUnstableContainer, "term %s", term)

		// C: OOM kills that also restart the container — cost rec held by the
		// crash gate while carrying both the OOM and the crash code.
		c := byKey["oom-c/"+term+"/cost"]
		assert.Equal(t, c.CurrentCPURequestMC, c.RecCPURequestMC, "term %s cost CPU held", term)
		assert.Equal(t, c.CurrentMemRequestKiB, c.RecMemRequestKiB, "term %s cost mem held", term)
		assert.Contains(t, c.NotificationCodes, types.NotifOOMDetected, "term %s", term)
		assert.Contains(t, c.NotificationCodes, types.NotifUnstableContainer, "term %s", term)

		// C performance engine: OOM bump path still upsizes memory.
		cp := byKey["oom-c/"+term+"/performance"]
		assert.Greater(t, cp.RecMemRequestKiB, cp.CurrentMemRequestKiB, "term %s OOM bump must upsize", term)
		assert.Contains(t, cp.NotificationCodes, types.NotifOOMDetected, "term %s", term)
		assert.Contains(t, cp.NotificationCodes, types.NotifUnstableContainer, "term %s", term)
	}
}
