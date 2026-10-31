package advisory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// Ports of the server's evaluate.go / quantile_test.go verdict-matrix cases
// (internal/engine/correlate) — the parity contract: same inputs, same
// verdicts. The silence cases are pinned exactly: unknown legs and
// pressured N produce nothing.

const hHC = "hhhhhhhh-1111-1111-1111-111111111111"

// sloSnapshot builds one cumulative snapshot row set for one collection
// time. counts maps le -> cumulative count.
func sloSnapshot(collected time.Time, counts map[float64]int64) []csv.SLORow {
	var out []csv.SLORow
	for _, le := range []float64{0.1, 0.5, 1.0, 2.0, 3.0, infLE()} {
		c := int64(0)
		if v, ok := counts[le]; ok {
			c = v
		}
		out = append(out, csv.SLORow{
			HCClusterID: hHC, WindowStart: collected.Add(-time.Hour), WindowEnd: collected,
			VerbGroup: "mutating", Le: le, BucketCount: c,
			CollectedAt: collected, Source: "kubernetes",
		})
	}
	return out
}

// hotSLO builds a p99 ~3s signature in the fire window plus calm baseline
// days: a baseline snapshot shortly before the window and a hot snapshot
// inside it (100m apart — the two-snapshot delta contract; the storage
// upsert collapses each window to its latest snapshot per (verb, le), so
// the pre-window snapshot is the delta baseline).
func hotSLO(windowStart time.Time) []csv.SLORow {
	var rows []csv.SLORow
	rows = append(rows, sloSnapshot(windowStart.Add(-50*time.Minute), map[float64]int64{
		0.1: 5, 0.5: 6, 1.0: 7, 2.0: 8, 3.0: 9, infLE(): 10,
	})...)
	rows = append(rows, sloSnapshot(windowStart.Add(50*time.Minute), map[float64]int64{
		0.1: 6, 0.5: 7, 1.0: 8, 2.0: 9, 3.0: 1010, infLE(): 1011,
	})...)
	return rows
}

// calmHourlySLO builds continuous hourly calm snapshots over
// [start, end): every observation lands in the 0.1 bucket (cumulative
// counts grow 100/hour on every le — a cumulative histogram with all
// mass below 0.1s). Continuity matters: each day-aggregated baseline
// window takes its delta baseline from the previous hour's snapshot
// (the 2h bound), so per-day pairs must chain.
func calmHourlySLO(start, end time.Time) []csv.SLORow {
	var rows []csv.SLORow
	k := int64(0)
	for t := start; t.Before(end); t = t.Add(time.Hour) {
		k++
		counts := map[float64]int64{}
		for _, le := range []float64{0.1, 0.5, 1.0, 2.0, 3.0, infLE()} {
			counts[le] = 100 * k
		}
		rows = append(rows, sloSnapshot(t, counts)...)
	}
	return rows
}

// headroomFixture builds the full known-evidence input: hot H in the fire
// window, calm 7-day baseline, saturated C, unpressured N.
func headroomFixture(now time.Time) HeadroomInput {
	windowEnd := now.UTC().Truncate(time.Hour)
	windowStart := windowEnd.Add(-time.Hour)
	// Calm hourly series over the fire window's preceding 8 days (the
	// single query span covers the window plus the 7-day baseline), then
	// the hot snapshot inside the window.
	slo := calmHourlySLO(windowStart.AddDate(0, 0, -8), windowStart)
	base := 100 * int64(len(slo)/6)
	slo = append(slo, sloSnapshot(windowStart.Add(50*time.Minute), map[float64]int64{
		0.1: base + 1, 0.5: base + 1, 1.0: base + 1, 2.0: base + 1,
		3.0: base + 1001, infLE(): base + 1001,
	})...)
	// C: mgmt digests over the assoc namespace, usage p95 high vs requests.
	end := now.UTC().Truncate(24 * time.Hour)
	var mgmt []types.KeyedDigest
	for d := 0; d < 3; d++ {
		day := end.AddDate(0, 0, -d)
		mgmt = append(mgmt, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: zAssocNS, Workload: "kube-apiserver", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: day, CPUUsageP95MC: 900, CPURequestP50MC: 1000},
		})
	}
	return HeadroomInput{
		HcID:               hHC,
		Now:                now,
		SLORows:            slo,
		MgmtDigests:        mgmt,
		AssocNamespaces:    []string{zAssocNS},
		NodeNotifications:  nil, // unpressured N: node recs exist, no 12/74
		NodeRecsExist:      true,
		SnapshotObserved:   windowStart.Add(2 * time.Minute),
		SnapshotConsistent: true,
		Policy:             DefaultHeadroomPolicy(),
	}
}

func TestHeadroom_FiresOnPainfulCPCalmWorkers(t *testing.T) {
	now := time.Now()
	got := EvaluateHeadroom(headroomFixture(now))
	require.NotNil(t, got, "H && C && !N all known must fire")
	assert.Equal(t, HeadroomVerdict, got.Verdict)
	assert.Equal(t, "high", got.Confidence)
	assert.Equal(t, hHC, got.HostedClusterID)
	p99 := got.Details["hosted_api_p99_seconds"].(float64)
	assert.GreaterOrEqual(t, p99, 2.0, "concentrated 3.0 bucket reads ~3s p99")
	assert.Less(t, p99, 3.01)
	assert.Equal(t, 0.30, got.Details["hosted_api_threshold_seconds"])
	assert.Equal(t, 0.9, got.Details["control_plane_capacity_ratio"])
	// Hourly window.
	assert.Equal(t, 1.0, got.WindowEnd.Sub(got.WindowStart).Hours())
}

func TestHeadroom_VerdictMatrix_Silence(t *testing.T) {
	now := time.Now()

	// Unknown H (no SLO rows): silence.
	in := headroomFixture(now)
	in.SLORows = nil
	assert.Nil(t, EvaluateHeadroom(in), "unknown H is silence")

	// Unknown C (no mgmt digests): silence.
	in = headroomFixture(now)
	in.MgmtDigests = nil
	assert.Nil(t, EvaluateHeadroom(in), "unknown C is silence")

	// Unknown N (no node recs): silence, never calm.
	in = headroomFixture(now)
	in.NodeRecsExist = false
	assert.Nil(t, EvaluateHeadroom(in), "unknown N is silence not calm")

	// Pressured N (overcommitted code): silence — keep worker advice.
	in = headroomFixture(now)
	in.NodeNotifications = []int16{12}
	assert.Nil(t, EvaluateHeadroom(in), "H && N keeps worker advice")

	// Calm H (all mass under the threshold): silence.
	in = headroomFixture(now)
	windowEnd := now.UTC().Truncate(time.Hour)
	ws := windowEnd.Add(-time.Hour)
	in.SLORows = calmHourlySLO(ws.AddDate(0, 0, -8), ws)
	assert.Nil(t, EvaluateHeadroom(in), "calm H blames nothing")

	// Calm C (low ratio): silence.
	in = headroomFixture(now)
	for i := range in.MgmtDigests {
		in.MgmtDigests[i].Row.CPUUsageP95MC = 100
	}
	assert.Nil(t, EvaluateHeadroom(in), "calm C blames nothing")

	// Stale snapshot (outside the window proof): silence.
	in = headroomFixture(now)
	in.SnapshotObserved = now.UTC().Truncate(time.Hour).Add(-5 * time.Hour)
	assert.Nil(t, EvaluateHeadroom(in), "the association must be proven FOR this window")

	// Inconsistent incarnation: silence.
	in = headroomFixture(now)
	in.SnapshotConsistent = false
	assert.Nil(t, EvaluateHeadroom(in), "a recreated HC fails the strict incarnation rule")
}

func TestHeadroom_BaselineMultipleRaisesThreshold(t *testing.T) {
	now := time.Now()
	in := headroomFixture(now)
	// Raise the baseline multiple so the calm days' median (0.1s-scale)
	// lifts the threshold above the hot p99: the fire becomes silence.
	in.Policy.HBaselineMultiple = 1000000
	assert.Nil(t, EvaluateHeadroom(in), "multiple-of-baseline threshold must gate the same p99")
}

func TestWindowP99_TwoSnapshotDelta(t *testing.T) {
	now := time.Now()
	windowEnd := now.UTC().Truncate(time.Hour)
	windowStart := windowEnd.Add(-time.Hour)
	rows := hotSLO(windowStart)
	groups, ok := WindowP99(sloSamples(rows), windowStart, windowEnd, 30*time.Minute)
	require.True(t, ok)
	p99, ok := groups["mutating"]
	require.True(t, ok)
	assert.GreaterOrEqual(t, p99, 2.0)
	assert.Less(t, p99, 3.01)
}

func TestWindowP99_SpanTooShortIsUnknown(t *testing.T) {
	now := time.Now()
	windowEnd := now.UTC().Truncate(time.Hour)
	windowStart := windowEnd.Add(-time.Hour)
	// Two snapshots only 10m apart: below the 30m min-data rule.
	rows := append(
		sloSnapshot(windowStart.Add(10*time.Minute), map[float64]int64{0.1: 5, infLE(): 10}),
		sloSnapshot(windowStart.Add(20*time.Minute), map[float64]int64{0.1: 6, infLE(): 100})...,
	)
	_, ok := WindowP99(sloSamples(rows), windowStart, windowEnd, 30*time.Minute)
	assert.False(t, ok, "the 30m min-data rule gates the span")
}

func TestWindowP99_NoBaselineIsUnknown(t *testing.T) {
	now := time.Now()
	windowEnd := now.UTC().Truncate(time.Hour)
	windowStart := windowEnd.Add(-time.Hour)
	// Snapshots only inside the window: no baseline strictly before start.
	rows := sloSnapshot(windowStart.Add(30*time.Minute), map[float64]int64{0.1: 5, infLE(): 10})
	_, ok := WindowP99(sloSamples(rows), windowStart, windowEnd, 0)
	assert.False(t, ok, "deltas need a pre-window baseline")
}

// sloSamples converts SLO rows to WindowP99 samples (all sources — the
// H-leg contract; the api-tax rule splits per source itself).
func sloSamples(rows []csv.SLORow) []Sample {
	out := make([]Sample, 0, len(rows))
	for _, r := range rows {
		out = append(out, Sample{Group: r.VerbGroup, LE: r.Le, Count: float64(r.BucketCount), Collected: r.CollectedAt})
	}
	return out
}
