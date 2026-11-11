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
			// #826: the usage level rides both percentiles — off rows keep
			// their veto semantics now that SleepSums consumes the p95.
			Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: usage, CPUUsageP95MC: usage},
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
	// #779 class (calendar-dependent fixture, surfaced by #760's
	// day-applicability change): a 2-day window anchored at time.Now() is
	// Sat+Sun once a week, when every day is schedule-non-applicable and the
	// BH floor is vacuous — the third assertion fails. Pin the anchor to a
	// Wednesday UTC so the window is Monday+Tuesday: both applicable days,
	// the floors actually gate.
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
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
			// #826: the usage level rides both percentiles — off rows keep
			// their veto semantics now that SleepSums consumes the p95.
			Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: usage, CPUUsageP95MC: usage},
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
			// #826: the usage level rides both percentiles — off rows keep
			// their veto semantics now that SleepSums consumes the p95.
			Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: usage, CPUUsageP95MC: usage},
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
			// #826: the usage level rides both percentiles — off rows keep
			// their veto semantics now that SleepSums consumes the p95.
			Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: usage, CPUUsageP95MC: usage},
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

// TestSleepCandidate_OffLegConsumesP95 is #826's offline repro: off legs
// carrying P50=0 / P95=2000 must veto. Baseline: SleepSums consumed the
// P50 for both legs, the summed evidence read 0, and the advisory fired.
func TestSleepCandidate_OffLegConsumesP95(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	var bhRows, offRows []types.KeyedDigest
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		bhRows = append(bhRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 300},
		})
		// Off leg: p50 0 (the median hides the nightly burst), p95 2000.
		offRows = append(offRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 0, CPUUsageP95MC: 2000},
		})
	}
	got := SleepCandidate("hc-826", TierNonProd, SleepSums(bhRows, offRows), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"the off leg's p95 (2000m nightly burst) must drive the veto — SleepSums consuming the p50 read 0 (#826)")
}

// TestSleepCandidate_LegacyOffRowsReadUnknown (#826 review F2): a legacy
// night-only workload (off rows with p50 but NO p95 — pre-#826 DB rows)
// must NOT vanish behind a re-ingested quiet sibling: SleepSumsAll routes
// the legacy rows through the #761 unknown gate.
func TestSleepCandidate_LegacyOffRowsReadUnknown(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	var bhRows, offRows, legacyRows []types.KeyedDigest
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		// Quiet sibling: fully migrated, BH-active, off-p95 = 0.
		bhRows = append(bhRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 300},
		})
		offRows = append(offRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 0, CPUUsageP95MC: 0},
		})
		// The legacy night-only workload: off p50 (activity) with NO p95.
		legacyRows = append(legacyRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "batch-jobs", Workload: "night-cron", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 2000},
		})
	}
	// Baseline hazard: SleepSums (without legacy) drops the night-cron
	// workload entirely and the quiet sibling covers every day.
	got := SleepCandidate("hc-826-legacy", TierNonProd, SleepSumsAll(bhRows, offRows, legacyRows), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"a legacy (pre-p95) night-only workload must read unknown — never vanish behind a re-ingested quiet sibling (#826)")
}

// TestSleepCandidate_PartialMigrationReadsUnknown (#826 review F1,
// offline): a workload-day split between measured and legacy off rows
// reads unmeasured — the #761 gate — matching the server's
// COUNT(p95) = COUNT(*) rule.
func TestSleepCandidate_PartialMigrationReadsUnknown(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	var bhRows, offRows, legacyRows []types.KeyedDigest
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		bhRows = append(bhRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 300},
		})
		// The split workload: c0 measured and quiet...
		offRows = append(offRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "rotator", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 0, CPUUsageP95MC: 0},
		})
		// ...c1 legacy with its activity in the p50.
		legacyRows = append(legacyRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "rotator", WorkloadType: "Deployment", ContainerName: "c1"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 2000},
		})
	}
	got := SleepCandidate("hc-826-partial", TierNonProd, SleepSumsAll(bhRows, offRows, legacyRows), testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"a partially-migrated workload must read unmeasured — the quiet measured container must not mask the legacy row's activity (#826)")
}

// TestSleepCandidate_SparseCoverageIsUnknown is #827's offline repro: off
// legs observed for only 2 of 16 window hours must read the day unknown —
// never complete coverage. The coverage rides the hour-count input the CLI
// computes; this test drives SleepCandidate through CoverageSums directly.
func TestSleepCandidate_SparseCoverageIsUnknown(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	var bhRows, offRows []types.KeyedDigest
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		bhRows = append(bhRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 300, CPUUsageP95MC: 300},
		})
		offRows = append(offRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 0, CPUUsageP95MC: 0},
		})
	}
	// Sparse on the OFF leg only (2 of 16): the BH leg sits at full
	// coverage (8 of 8) so the test isolates the off-leg gate — removing
	// only that gate must make it fail (#827 review: leg isolation).
	cov := DayCoverage{}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		key := d.UTC().Format("2006-01-02")
		if cov[key] == nil {
			cov[key] = map[DayKey]LegCoverage{}
		}
		cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 8, OffHours: 2}
	}
	got := SleepCandidateWithCoverage("hc-827", TierNonProd, SleepSums(bhRows, offRows), cov, true, testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"2-of-16 observed off-hours must read the day unknown — never complete coverage (#827)")
}

// TestSleepCandidate_SparseBHCoverageIsUnknown isolates the BH leg of the
// #827 floor: off coverage full (16 of 16), BH coverage sparse (1 of 8) —
// removing only the BH gate must make it fail.
func TestSleepCandidate_SparseBHCoverageIsUnknown(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	var bhRows, offRows []types.KeyedDigest
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		bhRows = append(bhRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 300, CPUUsageP95MC: 300},
		})
		offRows = append(offRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 0, CPUUsageP95MC: 0},
		})
	}
	cov := DayCoverage{}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		key := d.UTC().Format("2006-01-02")
		if cov[key] == nil {
			cov[key] = map[DayKey]LegCoverage{}
		}
		cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 1, OffHours: 16}
	}
	got := SleepCandidateWithCoverage("hc-827", TierNonProd, SleepSums(bhRows, offRows), cov, true, testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"1-of-8 observed BH-hours must read the day unknown — the BH leg of the floor (#827)")
}

// TestSleepCandidate_CoverageFloorBoundary is #827's boundary control: with
// the 09:00–17:00 seven-day schedule the off window is 16h and the 0.5
// floor demands 8 observed off-hours. Exactly at the floor the advisory
// must still fire (a floor is a minimum, not a ceiling); one hour below
// reads the day unknown. Also pins the per-day keying: one sparse day among
// fourteen full ones must read unknown — a workload-wide coverage value
// would mask it (the day-collision defect the fresh review of the first
// draft caught). A seven-day schedule keeps the boundary uniform: a
// weekday-only schedule gives weekends a 24h off window (floor 12), so
// "at floor" is not one number across the week.
func TestSleepCandidate_CoverageFloorBoundary(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	sched := testSchedule()
	sched.Days = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
	mkSums := func() map[string]map[DayKey]DaySum {
		var bhRows, offRows []types.KeyedDigest
		for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
			bhRows = append(bhRows, types.KeyedDigest{
				Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
				Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 300, CPUUsageP95MC: 300},
			})
			offRows = append(offRows, types.KeyedDigest{
				Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
				Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 0, CPUUsageP95MC: 0},
			})
		}
		return SleepSums(bhRows, offRows)
	}
	fullCov := func(off int16) DayCoverage {
		cov := DayCoverage{}
		for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
			key := d.UTC().Format("2006-01-02")
			if cov[key] == nil {
				cov[key] = map[DayKey]LegCoverage{}
			}
			cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 8, OffHours: off}
		}
		return cov
	}

	// At the floor: 8 of 16 off-hours, 8 of 8 BH-hours → fires.
	got := SleepCandidateWithCoverage("hc-827", TierNonProd, mkSums(), fullCov(8), true, sched, DefaultSleepPolicy(), 2, nil, nil, now)
	assert.NotNil(t, got,
		"8-of-16 observed off-hours is exactly the 0.5 floor — the advisory must fire")

	// One below: 7 of 16 → unknown.
	got = SleepCandidateWithCoverage("hc-827", TierNonProd, mkSums(), fullCov(7), true, sched, DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"7-of-16 observed off-hours is below the 0.5 floor — the day must read unknown")

	// Day collision: one sparse day among fourteen full ones → unknown.
	sparseDay := DayCoverage{}
	d := start
	for i := 0; i < 14; i++ {
		key := d.UTC().Format("2006-01-02")
		sparseDay[key] = map[DayKey]LegCoverage{}
		if i == 5 {
			sparseDay[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 8, OffHours: 2}
		} else {
			sparseDay[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 8, OffHours: 16}
		}
		d = d.AddDate(0, 0, 1)
	}
	got = SleepCandidateWithCoverage("hc-827", TierNonProd, mkSums(), sparseDay, true, sched, DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"one sparse day among fourteen full ones must read unknown — coverage is per day, never workload-wide")
}

// TestSleepCandidate_OvernightScheduleCoverageFires (#827 review F4/F5):
// a Mon–Fri 22:00–06:00 schedule splits across UTC day buckets — Monday
// holds only its 22:00 and 23:00 buckets (2 BH hours; the post-midnight
// portion belongs to Tuesday), Tuesday–Friday hold 8, Saturday 6 (the
// Friday shift's post-midnight portion), Sunday 0. Window-length
// arithmetic (8h BH window → floor 4) would demand more than Monday can
// produce and suppress the advisory even under complete observation; the
// classification-based derivation must let full coverage fire.
func TestSleepCandidate_OvernightScheduleCoverageFires(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	sched := testSchedule()
	sched.StartTime = "22:00"
	sched.EndTime = "06:00"
	// bhPossible per UTC weekday: Mon 2, Tue–Fri 8, Sat 6, Sun 0.
	bhPossible := map[time.Weekday]int{
		time.Monday: 2, time.Tuesday: 8, time.Wednesday: 8, time.Thursday: 8,
		time.Friday: 8, time.Saturday: 6, time.Sunday: 0,
	}
	var bhRows, offRows []types.KeyedDigest
	cov := DayCoverage{}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		bhRows = append(bhRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 300, CPUUsageP95MC: 300},
		})
		offRows = append(offRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 0, CPUUsageP95MC: 0},
		})
		key := d.UTC().Format("2006-01-02")
		if cov[key] == nil {
			cov[key] = map[DayKey]LegCoverage{}
		}
		bh := int16(bhPossible[d.UTC().Weekday()]) //nolint:gosec // G115: fixture lookup, values ≤ 24 by construction
		cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: bh, OffHours: 24 - bh}
	}
	got := SleepCandidateWithCoverage("hc-827", TierNonProd, SleepSums(bhRows, offRows), cov, true, sched, DefaultSleepPolicy(), 2, nil, nil, now)
	assert.NotNil(t, got,
		"complete observation on an overnight schedule must fire — the floor derives per-day possible hours, never the window length (#827 review F4/F5)")
}

// TestSleepCandidate_FractionalWindowCoverageFires (#827 review F4): a
// 22:30–22:00 schedule wraps almost the whole day — 23 BH hour buckets,
// 1 off bucket. The floor must demand 1 off hour (ceil of 0.5·1), not the
// 12 a zero-BH-window misderivation would demand.
func TestSleepCandidate_FractionalWindowCoverageFires(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	sched := testSchedule()
	sched.Days = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
	sched.StartTime = "22:30"
	sched.EndTime = "22:00"
	var bhRows, offRows []types.KeyedDigest
	cov := DayCoverage{}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		bhRows = append(bhRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 300, CPUUsageP95MC: 300},
		})
		offRows = append(offRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 0, CPUUsageP95MC: 0},
		})
		key := d.UTC().Format("2006-01-02")
		if cov[key] == nil {
			cov[key] = map[DayKey]LegCoverage{}
		}
		cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 23, OffHours: 1}
	}
	got := SleepCandidateWithCoverage("hc-827", TierNonProd, SleepSums(bhRows, offRows), cov, true, sched, DefaultSleepPolicy(), 2, nil, nil, now)
	assert.NotNil(t, got,
		"complete observation on a 22:30–22:00 schedule must fire — 1 observed off-hour meets the ceil(0.5·1) floor (#827 review F4)")
}

// TestSleepCandidate_UnattestedScheduleIsSilent (#831): stored evidence
// split under a window other than the one this evaluation resolves must
// not produce advice — the owner-approved silence-over-wrong trade. The
// DB mode computes the flag from the rows' fingerprints; the CSV mode is
// inherently attested.
func TestSleepCandidate_UnattestedScheduleIsSilent(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	var bhRows, offRows []types.KeyedDigest
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		bhRows = append(bhRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 300, CPUUsageP95MC: 300},
		})
		offRows = append(offRows, types.KeyedDigest{
			Key: types.ContainerKey{Namespace: "demo-app", Workload: "web", WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: d, CPUUsageP50MC: 0, CPUUsageP95MC: 0},
		})
	}
	cov := DayCoverage{}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		key := d.UTC().Format("2006-01-02")
		if cov[key] == nil {
			cov[key] = map[DayKey]LegCoverage{}
		}
		cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 8, OffHours: 16}
	}
	// Attested: the signature evaluates normally (the quiet positive).
	got := SleepCandidateWithCoverage("hc-831", TierNonProd, SleepSums(bhRows, offRows), cov, true, testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.NotNil(t, got, "attested evidence must evaluate normally")
	// Unattested: silence, regardless of the otherwise-qualifying signature.
	got = SleepCandidateWithCoverage("hc-831", TierNonProd, SleepSums(bhRows, offRows), cov, false, testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"evidence split under a different window must read unknown — never wrong-window advice (#831)")
}
