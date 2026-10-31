package advisory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// Ports of the server's sleep_test.go cases (internal/engine/correlate) —
// the parity contract: same inputs, same verdicts. Distinct values per
// field so a mis-mapping cannot pass by coincidence.

func testSchedule() bhschedule.Schedule {
	return bhschedule.Schedule{
		Timezone:  "UTC",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "09:00",
		EndTime:   "17:00",
		Enabled:   true,
	}
}

// bhFixture builds per-day digest rows: one user workload (busy in hours,
// idle outside via outsideMC), plus busy platform rows on both streams
// (liveness + the shared exclusion proof).
func bhFixture(now time.Time, bh, outside int64, days int) ([]types.KeyedDigest, []types.KeyedDigest) {
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -days)
	var all, bhRows []types.KeyedDigest
	mk := func(day time.Time, ns, wl string, usage int64, isBH bool) {
		kd := types.KeyedDigest{
			Key: types.ContainerKey{Namespace: ns, Workload: wl, WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: usage},
		}
		if isBH {
			bhRows = append(bhRows, kd)
		} else {
			all = append(all, kd)
		}
	}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		mk(d, "demo-app", "web", bh, true)
		mk(d, "demo-app", "web", bh+outside, false)
		mk(d, "kube-system", "coredns", 2000, true)
		mk(d, "kube-system", "coredns", 2100, false)
	}
	return all, bhRows
}

func TestSleepCandidate_DeclaredAndBHSignatureFires(t *testing.T) {
	now := time.Now()
	all, bh := bhFixture(now, 300, 0, 14)
	sums := DaySums(all, bh)
	got := SleepCandidate("hc-a", TierNonProd, sums, testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	require.NotNil(t, got)
	assert.Equal(t, SleepVerdict, got.Verdict)
	assert.Equal(t, "high", got.Confidence)
	assert.Equal(t, int64(300), got.Details["bh_active_peak_cpu_mc"])
	assert.Equal(t, int64(0), got.Details["outside_bh_peak_cpu_mc"])
	assert.Equal(t, 2, got.Details["worker_node_count"])
	// Mon-fri 09:00-17:00: 16 outside-hours/day * 5 days = 80/week * 2 nodes.
	assert.Equal(t, 160.0, got.Details["reclaim_node_hours_per_week"])
	assert.Equal(t, "non-prod", got.Details["declared_tier"])
	require.NotEmpty(t, got.Evidence)
}

func TestSleepCandidate_DeclaredOvernightActiveIsSilent(t *testing.T) {
	now := time.Now()
	all, bh := bhFixture(now, 300, 200, 14)
	got := SleepCandidate("hc-b", TierNonProd, DaySums(all, bh), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got, "overnight activity must break the signature — the trap fixture")
}

func TestSleepCandidate_UnlabeledIsSilent(t *testing.T) {
	now := time.Now()
	all, bh := bhFixture(now, 300, 0, 14)
	got := SleepCandidate("hc-c", "", DaySums(all, bh), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got, "declared-only in v1")
	got = SleepCandidate("hc-c", "prod", DaySums(all, bh), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got, "non-prod is the only qualifying tier")
}

func TestSleepCandidate_UnknownDaysSilent(t *testing.T) {
	now := time.Now()
	// 13 days of both streams + 1 all-hours-only day: partial BH coverage
	// reads unknown, never zero-activity.
	all, bh := bhFixture(now, 300, 0, 13)
	end := now.UTC().Truncate(24 * time.Hour)
	day13 := end.AddDate(0, 0, -1)
	all = append(all, types.KeyedDigest{
		Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
		Row: types.DigestRow{BucketDate: day13, CPUUsageP50MC: 300},
	})
	got := SleepCandidate("hc-d", TierNonProd, DaySums(all, bh), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got)
}

func TestSleepCandidate_IdleAlwaysIsSilent(t *testing.T) {
	now := time.Now()
	// Zero-activity user workload on both streams: no BH activity — the
	// predicate partition routes idle-always to the zombie rule (#722),
	// never to sleep.
	all, bh := bhFixture(now, 0, 0, 14)
	got := SleepCandidate("hc-e", TierNonProd, DaySums(all, bh), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got)
}

func TestSleepCandidate_NonDefaultFloors(t *testing.T) {
	now := time.Now()
	all, bh := bhFixture(now, 300, 50, 2)
	sums := DaySums(all, bh)
	pol := DefaultSleepPolicy()
	pol.WindowDays = 2 // match the 2-day fixture (also pins the window knob)

	// Default floors: BH-active (300 >= 10) but outside 50 >= 10 breaks.
	assert.Nil(t, SleepCandidate("hc-f", TierNonProd, sums, testSchedule(), pol, 2, nil, nil, now))

	// Non-default outside floor 100: outside 50 < 100 admits it.
	pol.OutsideBHIdleFloorMC = 100
	assert.NotNil(t, SleepCandidate("hc-f", TierNonProd, sums, testSchedule(), pol, 2, nil, nil, now),
		"non-default outside floor must admit outside 50 — config-derived, not hardcoded")

	// Non-default BH floor 500: 300 < 500 → not active in hours.
	pol.BHActivityFloorMC = 500
	assert.Nil(t, SleepCandidate("hc-f", TierNonProd, sums, testSchedule(), pol, 2, nil, nil, now),
		"non-default BH floor must reject activity 300")
}

func TestSleepCandidate_SyncedExclusionsShared(t *testing.T) {
	now := time.Now()
	// All user workloads in the synced set: nothing counts as user
	// activity → no BH-active day → silent (the synced set widens
	// exclusion exactly like the server's zombie rule).
	all, bh := bhFixture(now, 300, 0, 14)
	got := SleepCandidate("hc-g", TierNonProd, DaySums(all, bh), testSchedule(), DefaultSleepPolicy(), 2,
		map[string]bool{"demo-app": true}, nil, now)
	assert.Nil(t, got)
}

func TestWeeklyOutsideHours_MidnightCrossing(t *testing.T) {
	s := testSchedule()
	s.StartTime = "22:00"
	s.EndTime = "06:00"
	// 8 inside-hours (22:00-06:00 crossing midnight), 16 outside per
	// day * 5 days = 80.
	assert.InDelta(t, 80.0, weeklyOutsideHours(s), 0.001)
}
