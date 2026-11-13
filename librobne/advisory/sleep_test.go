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
	// #848: the weekly complement — Mon-fri 09:00-17:00 has 40 BH
	// hours/week; the sleep window is the remaining 168-40 = 128
	// hours/week (non-scheduled days are slept too — weekend activity
	// is a veto) * 2 nodes.
	assert.Equal(t, 256.0, got.Details["reclaim_node_hours_per_week"])
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

// TestWeeklyOutsideHours (#848): the weekly complement — 168 minus the
// week's business hours. Non-scheduled days are slept in full (their
// activity is a veto), so they count toward the reclaim; each
// scheduled day contributes exactly its window (overnight wrap
// attributes the post-midnight portion to the scheduled day's shift).
func TestWeeklyOutsideHours_MidnightCrossing(t *testing.T) {
	weekday := testSchedule() // Mon-Fri 09:00-17:00: 40 BH/week
	assert.InDelta(t, 128.0, weeklyOutsideHours(weekday), 0.001,
		"Mon-Fri 09:00-17:00: 168-40 = 128 sleep hours/week")

	overnight := testSchedule()
	overnight.StartTime = "22:00"
	overnight.EndTime = "06:00" // 8h window: 40 BH/week
	assert.InDelta(t, 128.0, weeklyOutsideHours(overnight), 0.001,
		"Mon-Fri 22:00-06:00: 8 inside-hours/shift * 5 = 40 BH; 168-40 = 128")

	alldays := testSchedule()
	alldays.Days = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
	assert.InDelta(t, 112.0, weeklyOutsideHours(alldays), 0.001,
		"7-day 09:00-17:00: 56 BH; 168-56 = 112")

	nodays := testSchedule()
	nodays.Days = nil
	assert.InDelta(t, 168.0, weeklyOutsideHours(nodays), 0.001,
		"no business hours at all: the full week is the sleep window")

	// #848 review P2: duplicate day entries pass API/CLI validation
	// while classification treats the list as a set — the count must
	// be distinct days, never list entries (entry-counting at a
	// near-24h window went negative).
	dupes := testSchedule()
	dupes.Days = []string{"monday", "monday", "monday", "monday", "monday", "monday", "monday", "monday"}
	assert.InDelta(t, 160.0, weeklyOutsideHours(dupes), 0.001,
		"eight monday entries are ONE distinct day: 168-8 = 160 (one 8h window), never negative")
	dupes.StartTime = "00:00"
	dupes.EndTime = "23:59"
	assert.InDelta(t, 144.0167, weeklyOutsideHours(dupes), 0.001,
		"near-24h window with duplicate entries: 168-23.983 = 144.017, never -23.87")
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
		cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 8, OffHours: 2, Attested: true}
	}
	got := SleepCandidateWithCoverage("hc-827", TierNonProd, SleepSums(bhRows, offRows), cov, testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
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
		cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 1, OffHours: 16, Attested: true}
	}
	got := SleepCandidateWithCoverage("hc-827", TierNonProd, SleepSums(bhRows, offRows), cov, testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
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
			cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 8, OffHours: off, Attested: true}
		}
		return cov
	}

	// At the floor: 8 of 16 off-hours, 8 of 8 BH-hours → fires.
	got := SleepCandidateWithCoverage("hc-827", TierNonProd, mkSums(), fullCov(8), sched, DefaultSleepPolicy(), 2, nil, nil, now)
	assert.NotNil(t, got,
		"8-of-16 observed off-hours is exactly the 0.5 floor — the advisory must fire")

	// One below: 7 of 16 → unknown.
	got = SleepCandidateWithCoverage("hc-827", TierNonProd, mkSums(), fullCov(7), sched, DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"7-of-16 observed off-hours is below the 0.5 floor — the day must read unknown")

	// Day collision: one sparse day among fourteen full ones → unknown.
	sparseDay := DayCoverage{}
	d := start
	for i := 0; i < 14; i++ {
		key := d.UTC().Format("2006-01-02")
		sparseDay[key] = map[DayKey]LegCoverage{}
		if i == 5 {
			sparseDay[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 8, OffHours: 2, Attested: true}
		} else {
			sparseDay[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 8, OffHours: 16, Attested: true}
		}
		d = d.AddDate(0, 0, 1)
	}
	got = SleepCandidateWithCoverage("hc-827", TierNonProd, mkSums(), sparseDay, sched, DefaultSleepPolicy(), 2, nil, nil, now)
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
		cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: bh, OffHours: 24 - bh, Attested: true}
	}
	got := SleepCandidateWithCoverage("hc-827", TierNonProd, SleepSums(bhRows, offRows), cov, sched, DefaultSleepPolicy(), 2, nil, nil, now)
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
		cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 23, OffHours: 1, Attested: true}
	}
	got := SleepCandidateWithCoverage("hc-827", TierNonProd, SleepSums(bhRows, offRows), cov, sched, DefaultSleepPolicy(), 2, nil, nil, now)
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
		cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 8, OffHours: 16, Attested: true}
	}
	// Attested: the signature evaluates normally (the quiet positive).
	got := SleepCandidateWithCoverage("hc-831", TierNonProd, SleepSums(bhRows, offRows), cov, testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.NotNil(t, got, "attested evidence must evaluate normally")
	// Unattested USER workload-day: silence, regardless of the otherwise-
	// qualifying signature (#831, workload-grain since #847).
	for day, pairs := range cov {
		for k, c := range pairs {
			c.Attested = false
			pairs[k] = c
		}
		cov[day] = pairs
	}
	got = SleepCandidateWithCoverage("hc-831", TierNonProd, SleepSums(bhRows, offRows), cov, testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"evidence split under a different window must read unknown — never wrong-window advice (#831)")
}

// --- #845: timezone-shifted sleep applicability (offline parity) ---

// shiftedLegs builds per-day evidence legs for a schedule whose BH hours
// land on shifted UTC days: the BH leg exists only where the
// classification finds BH hours (the writer's leg-split shape).
// lowWeekday (if valid) marks UTC days whose user-workload BH leg sits
// below the activity floor — present but idle, distinct from absent.
// shiftedLegs seeds the standard active-in-hours / idle-outside user
// signature (bh 300, off 0 — the bhFixture values) under a
// timezone-shifted schedule; day-level variation rides lowWeekday
// (present-but-idle BH leg).
func shiftedLegs(sched bhschedule.Schedule, lowWeekday time.Weekday) (bhRows, offRows []types.KeyedDigest) {
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	mk := func(day time.Time, ns, wl string, usage int64, isBH bool) {
		kd := types.KeyedDigest{
			Key: types.ContainerKey{Namespace: ns, Workload: wl, WorkloadType: "Deployment", ContainerName: "c0"},
			Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: usage, CPUUsageP95MC: usage},
		}
		if isBH {
			bhRows = append(bhRows, kd)
		} else {
			offRows = append(offRows, kd)
		}
	}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		bhPossible := 0
		for h := 0; h < 24; h++ {
			if bhschedule.InBusinessHours(d.UTC().Add(time.Duration(h)*time.Hour), sched) {
				bhPossible++
			}
		}
		if bhPossible > 0 {
			usage := int64(300)
			if d.UTC().Weekday() == lowWeekday {
				usage = 0 // below the 10 mC activity floor, leg still present
			}
			mk(d, "demo-app", "web", usage, true)
			mk(d, "kube-system", "coredns", 2000, true)
		}
		mk(d, "demo-app", "web", 0, false)
		mk(d, "kube-system", "coredns", 100, false)
	}
	return
}

// kiritimatiMidnight: Pacific/Kiritimati (+14) Mon–Fri 00:00–08:00 local
// → BH Sun–Thu 10:00–18:00 UTC. Friday UTC: zero BH hours,
// DayAllowed(Friday)=true — #845 direction 1.
func kiritimatiMidnight() bhschedule.Schedule {
	return bhschedule.Schedule{
		Timezone:  "Pacific/Kiritimati",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "00:00", EndTime: "08:00", Enabled: true,
	}
}

// kiritimatiDaytime: Pacific/Kiritimati (+14) Mon–Fri 09:00–17:00 local
// → Sun 19:00–24:00 UTC (5 buckets), Mon–Thu (8 each), Fri 00:00–03:00
// (3). Sunday UTC: real BH hours, DayAllowed(Sunday)=false — #845
// direction 2.
func kiritimatiDaytime() bhschedule.Schedule {
	return bhschedule.Schedule{
		Timezone:  "Pacific/Kiritimati",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "09:00", EndTime: "17:00", Enabled: true,
	}
}

// TestSleepCandidate_TimezoneShiftedScheduleFires is #845's direction-1
// offline repro: a fully observed +14 schedule must qualify — baseline:
// the UTC-weekday check demands BH evidence on a zero-BH UTC day and the
// window never qualifies.
func TestSleepCandidate_TimezoneShiftedScheduleFires(t *testing.T) {
	now := time.Now()
	bh, off := shiftedLegs(kiritimatiMidnight(), -1)
	got := SleepCandidate("hc-845-shift", TierNonProd, SleepSums(bh, off), kiritimatiMidnight(), DefaultSleepPolicy(), 2, nil, nil, now)
	require.NotNil(t, got,
		"a fully observed timezone-shifted schedule must qualify — applicability derives from the classified BH hours, not the UTC weekday (#845)")
	assert.Equal(t, SleepVerdict, got.Verdict)
}

// TestSleepCandidate_TimezoneShiftedLenientDayRequiresBH is #845's
// direction-2 offline repro: a +14 daytime schedule puts real BH hours on
// Sunday UTC; a user workload idle during exactly those hours must break
// the signature — baseline: DayAllowed(Sunday)=false skips the BH gates.
func TestSleepCandidate_TimezoneShiftedLenientDayRequiresBH(t *testing.T) {
	now := time.Now()
	bh, off := shiftedLegs(kiritimatiDaytime(), time.Sunday)
	got := SleepCandidate("hc-845-lenient", TierNonProd, SleepSums(bh, off), kiritimatiDaytime(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"a UTC day carrying classified BH hours must require BH activity — the UTC weekday must not skip the gate (#845)")
}

// TestSleepCandidate_DisabledScheduleOffOnlyIsSilent pins the offline
// disabled contract (#845 owner decision): a disabled schedule classifies
// zero BH hours, so the writer's evidence is off-only — the advice must
// never fire. The defensive !Enabled guard keeps the BH gates ON, so the
// missing BH leg reads unknown rather than off-leg-only qualification.
func TestSleepCandidate_DisabledScheduleOffOnlyIsSilent(t *testing.T) {
	now := time.Now()
	disabled := kiritimatiMidnight()
	disabled.Enabled = false
	// The writer's shape for a disabled schedule: classification finds no
	// BH hours anywhere, so legs are off-only.
	bh, off := shiftedLegs(disabled, -1)
	assert.Empty(t, bh, "fixture sanity: a disabled schedule produces no BH legs")
	got := SleepCandidate("hc-845-disabled", TierNonProd, SleepSums(bh, off), disabled, DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got, "a disabled schedule must never advise — the BH gates stay on (advice-without-schedule)")
}

// utcOvernight: Mon–Fri 22:00–06:00 UTC gives Saturday six BH buckets
// from Friday's post-midnight shift while DayAllowed(Saturday)=false
// (#845 review P2).
func utcOvernight() bhschedule.Schedule {
	return bhschedule.Schedule{
		Timezone:  "UTC",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "22:00", EndTime: "06:00", Enabled: true,
	}
}

// utcSubHour: a same-day window contained in one clock hour classifies
// zero top-of-hour bucket starts on every day while real business time
// exists (#845 review P1).
func utcSubHour() bhschedule.Schedule {
	return bhschedule.Schedule{
		Timezone:  "UTC",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "09:15", EndTime: "09:45", Enabled: true,
	}
}

// TestSleepCandidate_OvernightSaturdayRequiresBH is the #845 review's
// P2 repro (offline): the post-midnight UTC day of an overnight schedule
// carries real BH hours; a user workload idle during exactly those
// Saturday hours must break the signature. Baseline (pre-#845):
// DayAllowed(Saturday)=false skipped the BH gates and the advice fired.
func TestSleepCandidate_OvernightSaturdayRequiresBH(t *testing.T) {
	now := time.Now()
	bh, off := shiftedLegs(utcOvernight(), time.Saturday)
	got := SleepCandidate("hc-845-ovn", TierNonProd, SleepSums(bh, off), utcOvernight(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"the post-midnight UTC day of an overnight schedule carries BH hours — the BH gates must apply (#845 review P2)")
}

// TestSleepCandidate_SubHourWindowOffOnlyIsSilent is the #845 review's
// P1 control (offline): a sub-hour window classifies zero bucket starts,
// so bucket-count applicability alone would qualify off-only idle
// evidence — false sleep advice breaking the zombie partition. The
// minute-granular intersection check keeps weekdays applicable; no BH
// row can exist, so the #761 gate reads the window unknown.
func TestSleepCandidate_SubHourWindowOffOnlyIsSilent(t *testing.T) {
	now := time.Now()
	bh, off := shiftedLegs(utcSubHour(), -1)
	assert.Empty(t, bh, "fixture sanity: a sub-hour window produces no BH bucket starts")
	got := SleepCandidate("hc-845-subhr", TierNonProd, SleepSums(bh, off), utcSubHour(), DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"a sub-hour window still has business hours — off-only idle evidence must read unknown, never sleep advice (#845 review P1)")
}

// TestSleepCandidate_ShiftedDayCoverageFloorIsUnknown is the #845
// review's P2 repro (offline): a timezone-shifted applicable day with a
// present, active BH leg below its coverage floor must read the day
// unknown — the #827 floor applies to newly applicable days. Without
// the pin, disabling the BH coverage-floor gate left every #845 test
// passing (the reviewer's mutation).
func TestSleepCandidate_ShiftedDayCoverageFloorIsUnknown(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	sched := kiritimatiMidnight()
	bh, off := shiftedLegs(sched, -1)
	// Full coverage everywhere except Sunday's BH leg: 2 of 8 possible
	// hours, below the 0.5 floor of 4.
	cov := DayCoverage{}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		key := d.UTC().Format("2006-01-02")
		if cov[key] == nil {
			cov[key] = map[DayKey]LegCoverage{}
		}
		cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 8, OffHours: 16, Attested: true}
		if d.UTC().Weekday() == time.Sunday {
			cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 2, OffHours: 16, Attested: true}
		}
	}
	got := SleepCandidateWithCoverage("hc-845-cov", TierNonProd, SleepSums(bh, off), cov, sched, DefaultSleepPolicy(), 2, nil, nil, now)
	assert.Nil(t, got,
		"a shifted applicable day below its BH coverage floor must read unknown — the #827 floor applies to newly applicable days (#845 review P2)")
}

// TestSleepCandidate_PlatformUnattestedDoesNotGate is #847's advisory-level
// parity control: a PLATFORM workload-day reading unattested must never
// silence advice built on attested USER evidence — the gate is
// user-workload-scoped, the server's shape.
func TestSleepCandidate_PlatformUnattestedDoesNotGate(t *testing.T) {
	now := time.Now()
	bh, off := bhFixture(now, 300, 0, 14)
	sums := SleepSums(bh, off)
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	cov := DayCoverage{}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		key := d.UTC().Format("2006-01-02")
		if cov[key] == nil {
			cov[key] = map[DayKey]LegCoverage{}
		}
		cov[key][DayKey{NS: "demo-app", WL: "web"}] = LegCoverage{BHHours: 8, OffHours: 16, Attested: true}
		// The platform workload's stale-split day — present in the sums,
		// unattested, and irrelevant to the gate.
		cov[key][DayKey{NS: "kube-system", WL: "coredns"}] = LegCoverage{BHHours: 8, OffHours: 16, Attested: false}
	}
	got := SleepCandidateWithCoverage("hc-847-adv", TierNonProd, sums, cov, testSchedule(), DefaultSleepPolicy(), 2, nil, nil, now)
	require.NotNil(t, got,
		"a platform workload's unattested day must not silence attested user evidence (#847 parity)")
	assert.Equal(t, SleepVerdict, got.Verdict)
}
