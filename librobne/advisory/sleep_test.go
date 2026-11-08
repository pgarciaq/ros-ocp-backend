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

// bhFixture builds per-day evidence legs (#760): one user workload (busy
// in hours, idle outside — the off leg is the direct outside-BH measure)
// plus platform rows on both legs. Distinct values so a mis-mapped leg
// cannot pass by coincidence.
func bhFixture(now time.Time, bh, outside int64, days int) (bhRows, offRows []types.KeyedDigest) {
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -days)
	mk := func(day time.Time, ns, wl string, usage int64, isBH bool) {
		kd := types.KeyedDigest{
			Key: types.ContainerKey{Namespace: ns, Workload: wl, WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: usage},
		}
		if isBH {
			bhRows = append(bhRows, kd)
		} else {
			offRows = append(offRows, kd)
		}
	}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		mk(d, "demo-app", "web", bh, true)
		mk(d, "demo-app", "web", outside, false)
		mk(d, "kube-system", "coredns", 2000, true)
		mk(d, "kube-system", "coredns", 100, false)
	}
	return bhRows, offRows
}

func TestSleepCandidate_DeclaredAndBHSignatureFires(t *testing.T) {
	now := time.Now()
	bh, off := bhFixture(now, 300, 0, 14)
	sums := SleepSums(bh, off)
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
	bh, off := bhFixture(now, 300, 200, 14)
	got := SleepCandidate("hc-b", TierNonProd, SleepSums(bh, off), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got, "overnight activity must break the signature — the trap fixture")
}

func TestSleepCandidate_UnlabeledIsSilent(t *testing.T) {
	now := time.Now()
	bh, off := bhFixture(now, 300, 0, 14)
	got := SleepCandidate("hc-c", "", SleepSums(bh, off), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got, "declared-only in v1")
	got = SleepCandidate("hc-c", "prod", SleepSums(bh, off), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got, "non-prod is the only qualifying tier")
}

func TestSleepCandidate_UnknownDaysSilent(t *testing.T) {
	now := time.Now()
	// 13 days of evidence inside the 14-day window: the absent day reads
	// unknown, never zero-activity.
	bh, off := bhFixture(now, 300, 0, 13)
	got := SleepCandidate("hc-d", TierNonProd, SleepSums(bh, off), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got)
}

func TestSleepCandidate_IdleAlwaysIsSilent(t *testing.T) {
	now := time.Now()
	// Zero-activity user workload on both streams: no BH activity — the
	// predicate partition routes idle-always to the zombie rule (#722),
	// never to sleep.
	bh, off := bhFixture(now, 0, 0, 14)
	got := SleepCandidate("hc-e", TierNonProd, SleepSums(bh, off), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got)
}

func TestSleepCandidate_NonDefaultFloors(t *testing.T) {
	now := time.Now()
	bh, off := bhFixture(now, 300, 50, 2)
	sums := SleepSums(bh, off)
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
	bh, off := bhFixture(now, 300, 0, 14)
	got := SleepCandidate("hc-g", TierNonProd, SleepSums(bh, off), testSchedule(), DefaultSleepPolicy(), 2,
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

// nightOnlyFixture is #761's (V15-11) offline repro shape: the BH-signature
// web workload plus an exclusively overnight CronJob with ONLY an off-window
// leg — the producer's normal shape (a workload with no in-window samples
// has no BH leg). Distinct values so a mis-attributed day cannot pass by
// coincidence.
func nightOnlyFixture(now time.Time, bh, outside int64, days int) (bhRows, offRows []types.KeyedDigest) {
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -days)
	mk := func(day time.Time, ns, wl string, usage int64, isBH bool) {
		kd := types.KeyedDigest{
			Key: types.ContainerKey{Namespace: ns, Workload: wl, WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: usage},
		}
		if isBH {
			bhRows = append(bhRows, kd)
		} else {
			offRows = append(offRows, kd)
		}
	}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		mk(d, "demo-app", "web", bh, true)
		mk(d, "demo-app", "web", outside, false)
		mk(d, "kube-system", "coredns", 2000, true)
		mk(d, "kube-system", "coredns", 100, false)
		// The night-only CronJob: off leg 2000m, NO BH leg.
		mk(d, "batch-jobs", "night-cron", 2000, false)
	}
	return bhRows, offRows
}

// TestSleepCandidate_NightOnlyNoBHRowIsSilent is #761's offline repro: a
// user workload with all-hours data and no BH row must make the day
// unknown — never silently skipped while the web sibling establishes the
// day's coverage. Baseline: high-confidence sleep advice for a cluster
// whose night job runs exactly in the hours the advisory would sleep away.
func TestSleepCandidate_NightOnlyNoBHRowIsSilent(t *testing.T) {
	now := time.Now()
	bh, off := nightOnlyFixture(now, 300, 0, 14)
	got := SleepCandidate("hc-761", TierNonProd, SleepSums(bh, off), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"a night-only user workload with no BH row must make the day unknown (#761)")
}

// Control: a system-namespace workload with a missing BH row stays skipped —
// the platform exclusion is preserved and a valid candidate still fires.
func TestSleepCandidate_SystemMissingBHRowStillFires(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	var bhRows, offRows []types.KeyedDigest
	mk := func(day time.Time, ns, wl string, usage int64, isBH bool) {
		kd := types.KeyedDigest{
			Key: types.ContainerKey{Namespace: ns, Workload: wl, WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: usage},
		}
		if isBH {
			bhRows = append(bhRows, kd)
		} else {
			offRows = append(offRows, kd)
		}
	}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		mk(d, "demo-app", "web", 300, true)
		mk(d, "demo-app", "web", 0, false)
		// System workload: off leg only (no BH leg).
		mk(d, "kube-system", "coredns", 2100, false)
	}
	got := SleepCandidate("hc-761-sys", TierNonProd, SleepSums(bhRows, offRows), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	require.NotNil(t, got, "a system workload's stream shape is irrelevant — the exclusion is preserved (#761)")
	assert.Equal(t, SleepVerdict, got.Verdict)
}

// Control: the reverse shape — a user workload with a BH row and NO
// all-hours row — also reads unknown after the fix.
func TestSleepCandidate_BHOnlyUserWorkloadIsSilent(t *testing.T) {
	now := time.Now()
	bh, off := nightOnlyFixture(now, 300, 0, 14)
	// Drop the night-cron rows from the off legs, then add the BH-ONLY
	// user workload to the BH legs (the reverse shape: BHMeasured=true,
	// OffMeasured=false).
	var filtered []types.KeyedDigest
	for _, kd := range off {
		if kd.Key.Namespace == "batch-jobs" {
			continue
		}
		filtered = append(filtered, kd)
	}
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		bh = append(bh, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "bh-only", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 250},
		})
	}
	got := SleepCandidate("hc-761-bhonly", TierNonProd, SleepSums(bh, filtered), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"a user workload with a BH leg and no off leg must read unknown (#761)")
}

// TestSleepCandidate_ConstantUsageIsSilent is #760's (V15-10) offline repro:
// constant 300m usage — the in-window p50 AND the off-window p50 are both
// 300 — the baseline's percentile subtraction read outside=0 and fired; the
// off-window p50 IS 300 and must veto.
func TestSleepCandidate_ConstantUsageIsSilent(t *testing.T) {
	now := time.Now()
	bh, off := bhFixture(now, 300, 300, 14)
	got := SleepCandidate("hc-760", TierNonProd, SleepSums(bh, off), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"constant 300m usage runs all night — the off-window leg is the direct outside-BH measure, never a subtraction (#760)")
}

// weekdayProducerLegs builds 14 days under a Mon-Fri schedule with the
// PRODUCER's true shape: weekdays carry both legs; weekends have no
// in-window hours, so no BH leg exists (#760).
func weekdayProducerLegs(bh, off int64) (bhRows, offRows []types.KeyedDigest) {
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	mk := func(day time.Time, ns, wl string, usage int64, isBH bool) {
		kd := types.KeyedDigest{
			Key: types.ContainerKey{Namespace: ns, Workload: wl, WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: usage},
		}
		if isBH {
			bhRows = append(bhRows, kd)
		} else {
			offRows = append(offRows, kd)
		}
	}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		weekend := d.UTC().Weekday() == time.Saturday || d.UTC().Weekday() == time.Sunday
		if !weekend {
			mk(d, "demo-app", "web", bh, true)
			mk(d, "kube-system", "coredns", 2000, true)
		}
		mk(d, "demo-app", "web", off, false)
		mk(d, "kube-system", "coredns", 100, false)
	}
	return bhRows, offRows
}

// TestSleepCandidate_WeekdayScheduleWeekendProducerShape (#760): a Mon-Fri
// schedule's weekends carry no BH leg (no in-window hours exist) — the
// BH-activity requirement is vacuous there; the off leg still gates. The
// digest-stream model read those days as unknown and the rule never fired
// for weekday schedules at all.
func TestSleepCandidate_WeekdayScheduleWeekendProducerShape(t *testing.T) {
	now := time.Now()
	bh, off := weekdayProducerLegs(300, 0)
	got := SleepCandidate("hc-760-wk", TierNonProd, SleepSums(bh, off), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	require.NotNil(t, got,
		"a weekday schedule with weekend off-only evidence must still fire — weekends have no business hours (#760)")
	assert.Equal(t, SleepVerdict, got.Verdict)
}

// Control: weekend off-window activity still vetoes (#760).
func TestSleepCandidate_WeekendActivityVetoes(t *testing.T) {
	now := time.Now()
	bh, off := weekdayProducerLegs(300, 200)
	got := SleepCandidate("hc-760-wkend", TierNonProd, SleepSums(bh, off), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"weekend off-window activity must veto — the advice sleeps those hours (#760)")
}
