// Package advisory computes the correlator advisories offline (robne CLI
// recommendation parity, #721–#724). The core is mode-agnostic: input
// assembly (files or database) happens in the CLI layer; this package owns
// the evaluation semantics, ported verbatim from the server's correlator.
//
// W8 non-prod sleep candidates (#721, ADR-0348): a hosted cluster qualifies
// when it is DECLARED non-prod (the tier label, latest-wins) AND shows the
// business-hours usage signature (user workloads active in hours, idle
// outside). The declaration classifies; the data gates — a labeled cluster
// with overnight workloads is never advised.
package advisory

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// SleepVerdict is the W8 sleep-candidate advisory verdict (#717's contract).
const SleepVerdict = "schedule_nonprod_sleep"

// TierNonProd is the insights.cost-management/hc-tier label value that
// declares an HC non-prod (ADR-0348 §2).
const TierNonProd = "non-prod"

// SleepPolicy carries the W8 evaluation knobs (the server's per-org
// correlator settings, offline as CLI config).
type SleepPolicy struct {
	WindowDays           int
	BHActivityFloorMC    int64
	OutsideBHIdleFloorMC int64
	// MinCoverageFrac (#827): the minimum fraction of a leg's window hours
	// that must be observed — the H-leg minimum-observation precedent. A
	// methodological floor, not a tenant knob.
	MinCoverageFrac float64
}

// DefaultSleepPolicy mirrors the server's compiled defaults
// (DefaultPolicy in internal/engine/correlate).
func DefaultSleepPolicy() SleepPolicy {
	return SleepPolicy{WindowDays: 14, BHActivityFloorMC: 10, OutsideBHIdleFloorMC: 10, MinCoverageFrac: 0.5}
}

// DayKey identifies one (namespace, workload) pair on one day.
type DayKey struct {
	NS string
	WL string
}

// DaySum is one (day, namespace, workload) pair across the two evidence
// legs (#760, #826): the in-window p50 sum and the off-window p95 sum —
// direct measures of each side of the schedule, never a subtraction. The
// off leg's statistic is p95 (#826): a median hides minority-of-window
// activity; the house convention for CPU-usage activity evidence is p95
// (#747's zombie-rule precedent). A day counts as covered only when BOTH
// legs are measured — partial business-hours coverage reads unknown, never
// zero-activity (ADR-0348 §5, amended #760).
type DaySum struct {
	BH          int64
	Off         int64
	BHMeasured  bool
	OffMeasured bool
}

// SleepSums groups the two evidence-leg digest sets into per-(day, ns,
// workload) CPU sums (millicores — the server's evidence input shape). bh
// carries in-window p50s; off carries off-window p95s (#826) — each
// computed through the canonical weighted digest machinery with a pure 0/1
// window filter (exact complements).
func SleepSums(bh, off []types.KeyedDigest) map[string]map[DayKey]DaySum {
	return SleepSumsAll(bh, off, nil)
}

// SleepSumsAll extends SleepSums with the legacy off legs (pre-#826 rows:
// off-p50 set, p95 NULL — see pgdigest.ReadSleepEvidence). A legacy row
// marks its workload-day's off leg PRESENT-but-UNMEASURED: OffMeasured is
// forced false, routing the workload through the #761 unknown gate — and a
// partially-migrated workload (some containers measured, some legacy) reads
// unmeasured too, matching the server's COUNT(p95) = COUNT(*) rule.
func SleepSumsAll(bh, off, offLegacy []types.KeyedDigest) map[string]map[DayKey]DaySum {
	out := map[string]map[DayKey]DaySum{}
	add := func(rows []types.KeyedDigest, isBH bool) {
		for _, kd := range rows {
			day := kd.Row.BucketDate.UTC().Format("2006-01-02")
			if out[day] == nil {
				out[day] = map[DayKey]DaySum{}
			}
			k := DayKey{NS: kd.Key.Namespace, WL: kd.Key.Workload}
			d := out[day][k]
			if isBH {
				d.BH += kd.Row.CPUUsageP50MC
				d.BHMeasured = true
			} else {
				d.Off += kd.Row.CPUUsageP95MC
				d.OffMeasured = true
			}
			out[day][k] = d
		}
	}
	add(bh, true)
	add(off, false)
	for _, kd := range offLegacy {
		day := kd.Row.BucketDate.UTC().Format("2006-01-02")
		if out[day] == nil {
			out[day] = map[DayKey]DaySum{}
		}
		k := DayKey{NS: kd.Key.Namespace, WL: kd.Key.Workload}
		d := out[day][k]
		d.OffMeasured = false
		out[day][k] = d
	}
	return out
}

// isUserNamespace applies the platform exclusion shared with the server's
// zombie rule: compiled prefixes/exact plus the optional synced set (the
// offline stand-in for the koku cost-groups push — absent input means the
// compiled defaults, which is itself a parity case).
func isUserNamespace(ns string, exact map[string]bool, prefixes []string) bool {
	if exact[ns] {
		return false
	}
	for _, p := range prefixes {
		if p != "" && len(ns) >= len(p) && ns[:len(p)] == p {
			return false
		}
	}
	return true
}

// CompiledPlatformPrefixes and CompiledPlatformExact mirror the server's
// zombiePlatformPrefixes/Exact (internal/engine/correlate/zombie.go) — the
// set BOTH the sleep and zombie rules share server-side. #722 restored the
// evidence-grounded lab addon entry the #721 copy dropped.
var (
	CompiledPlatformPrefixes = []string{"kube-", "openshift-"}
	CompiledPlatformExact    = map[string]bool{
		"koku-metrics-operator":               true,
		"local-path-storage":                  true,
		"open-cluster-management-agent-addon": true,
		"open-cluster-management-hc01":        true,
	}
)

// SleepEvidence is the typed evidence on a fired advisory (#717's details
// mapping; the raw strings ride Evidence for provenance).
type SleepEvidence struct {
	BHActivePeakMC          int64
	OutsidePeakMC           int64
	WorkerNodes             int
	ReclaimNodeHoursPerWeek float64
}

// LegCoverage is one (day, namespace, workload) pair's observed-hour
// counts per leg (#827). Zero means genuinely unobserved (or a container
// of the pair missing the leg entirely — the CLI's MIN treats absent as
// 0); the predicate compares against the window's floor. Absent from the
// coverage map = no coverage info (legacy rows) — the floor is skipped
// for that pair.
type LegCoverage struct {
	BHHours  int16
	OffHours int16

	// Attested (#831/#847): the workload-day's evidence rows all carry
	// the currently resolved schedule's fingerprint. User-workload-
	// scoped gate — platform rows never gate (the server's shape). A
	// pair ABSENT from cov carries no attestation info — the gate
	// skips (nil-coverage callers evaluate as attested).
	Attested bool
}

// DayCoverage maps day → (namespace, workload) → per-leg observed hours —
// the same shape as the sums input, so coverage is evaluated per day: a
// workload sparse on one day and fully observed on another must not read
// fully covered off the full day's counts.
type DayCoverage map[string]map[DayKey]LegCoverage

// SleepCandidate evaluates one hosted cluster. Returns nil when the HC does
// not qualify (undeclared, no schedule, unknown days, or the signature
// breaks) — silence, mirroring the server's never-fail-on-evidence posture.
// Coverage and schedule attestation are not evaluated by this entry point;
// use SleepCandidateWithCoverage when the caller can supply observation
// hours or reads stored evidence (#827, #831).
func SleepCandidate(hcID, tier string, sums map[string]map[DayKey]DaySum, sched bhschedule.Schedule, pol SleepPolicy, workerNodes int, syncedExact map[string]bool, syncedPrefixes []string, now time.Time) *Advisory {
	return SleepCandidateWithCoverage(hcID, tier, sums, nil, sched, pol, workerNodes, syncedExact, syncedPrefixes, now)
}

// SleepCandidateWithCoverage is SleepCandidate with the #827 observation
// coverage input (per-leg observed hours keyed by (day, ns, wl)) carrying
// the #831/#847 schedule attestation per workload-day: an unattested USER
// workload-day (evidence rows split under a window other than the one this
// evaluation resolves) reads unknown, never wrong-window advice (the
// owner-approved silence-over-wrong trade) — user-workload-scoped like the
// coverage gate, matching the server; a stale PLATFORM row never gates
// (#847 parity). The CSV mode builds coverage with Attested true (split
// and evaluation share the current schedule); the DB mode AND-collapses
// the rows' fingerprints in its coverage assembly. Pairs absent from cov
// carry no attestation info — the gate skips them (nil-coverage callers
// evaluate as attested, the pre-#827 shape).
func SleepCandidateWithCoverage(hcID, tier string, sums map[string]map[DayKey]DaySum, cov DayCoverage, sched bhschedule.Schedule, pol SleepPolicy, workerNodes int, syncedExact map[string]bool, syncedPrefixes []string, now time.Time) *Advisory {
	if tier != TierNonProd {
		return nil
	}
	exact := make(map[string]bool, len(CompiledPlatformExact)+len(syncedExact))
	for k, v := range CompiledPlatformExact {
		exact[k] = v
	}
	for k, v := range syncedExact {
		exact[k] = v
	}
	prefixes := append(append([]string(nil), CompiledPlatformPrefixes...), syncedPrefixes...)

	windowEnd := now.UTC().Truncate(24 * time.Hour)
	windowStart := windowEnd.AddDate(0, 0, -pol.WindowDays)

	var ev SleepEvidence
	for d := windowStart; d.Before(windowEnd); d = d.AddDate(0, 0, 1) {
		key := d.UTC().Format("2006-01-02")
		rows, ok := sums[key]
		if !ok {
			return nil // unknown day — never idle-by-absence
		}
		// #827: the coverage floor (the H-leg minimum-observation
		// precedent) — a user workload observed for fewer than
		// SleepMinCoverageFrac of a leg's possible hours makes the day
		// unknown. Possible hours derive by classifying each UTC hour
		// bucket of the day with bhschedule.InBusinessHours — the same
		// rule the ingestion writer's leg split and the server's
		// predicate apply — so overnight windows (split across UTC day
		// buckets) and non-applicable days (zero BH hours, floor
		// vacuous) are exact. The floor rounds up. Pairs absent from
		// cov carry no coverage info.
		bhPossible := 0
		for h := 0; h < 24; h++ {
			if bhschedule.InBusinessHours(d.UTC().Add(time.Duration(h)*time.Hour), sched) {
				bhPossible++
			}
		}
		// #845: applicability derives from the classified possible hours
		// — not a separate UTC-weekday check. The producer classifies in
		// the schedule's LOCAL timezone, so a timezone-shifted schedule's
		// BH hours can land on UTC days whose weekday the day list does
		// not name, and leave named weekdays BH-free: a UTC-weekday check
		// both demands BH evidence where no BH hours exist (the window
		// reads unknown forever) and skips the gates where BH hours do
		// exist. A day is applicable exactly when it carries BH hours;
		// #760's weekends are the zero-BH-hours case — the off leg still
		// gates safety (the advice sleeps those hours). Same-day UTC
		// schedules agree with the weekday rule bucket-for-bucket;
		// overnight schedules legitimately change on the post-midnight
		// UTC day (a Mon–Fri 22:00–06:00 UTC schedule gives Saturday six
		// BH buckets from Friday's shift while DayAllowed(Saturday) is
		// false — the same lenient-direction repair, pinned by test).
		// Sub-hour windows contained in one clock hour classify zero
		// bucket starts, so the minute-granular intersection check keeps
		// them applicable (#845 review P1). A disabled schedule keeps the
		// BH gates on (the server-side nil contract; the server writer
		// emits no rows when disabled, the CSV path off-only legs): such
		// days read unknown — never advice-without-schedule.
		dayApplicable := !sched.Enabled || bhPossible > 0 ||
			bhschedule.DayIntersectsBusinessHours(d.UTC(), sched)

		offPossible := 24 - bhPossible
		minBHHours := int(math.Ceil(pol.MinCoverageFrac * float64(bhPossible)))
		minOffHours := int(math.Ceil(pol.MinCoverageFrac * float64(offPossible)))
		covered := false
		dayBHActive := false
		for k, r := range rows {
			missingBH := !r.BHMeasured
			if !dayApplicable {
				missingBH = false
			}
			if missingBH || !r.OffMeasured {
				// #761 (V15-11): a USER workload with exactly one stream
				// measured makes the day unknown — never silently skipped
				// while a sibling covers the day. Absence of a BH row does
				// not prove zero off-hours usage: the producer omits
				// all-zero-BH groups by design (the normal night-only
				// shape), so the off-hours split is unknowable — unknown,
				// never zero-activity (ADR-0348 §5, workload grain).
				if isUserNamespace(k.NS, exact, prefixes) {
					return nil
				}
				continue // system workload: stream shape irrelevant
			}
			// #827: the coverage floor gate — user workloads only (system
			// rows carry no user-activity evidence). Per-day lookup: a
			// pair absent from cov carries no coverage info.
			if c, ok := cov[key][k]; ok && isUserNamespace(k.NS, exact, prefixes) {
				if dayApplicable && int(c.BHHours) < minBHHours {
					return nil
				}
				if int(c.OffHours) < minOffHours {
					return nil
				}
				// #831/#847: the schedule-attestation gate, user-workload-
				// scoped like the floor above — a USER workload-day whose
				// evidence rows were split under a different window reads
				// unknown, never wrong-window advice (the owner-approved
				// silence-over-wrong trade). PLATFORM rows never gate:
				// the server excludes them before this gate, and one
				// stale platform row must not silence rebuilt user
				// evidence (the #847 parity fix — the window-wide
				// boolean this replaces broke that parity).
				if !c.Attested {
					return nil
				}
			}
			covered = true
			if !isUserNamespace(k.NS, exact, prefixes) {
				continue
			}
			// #760 (V15-10) + #826 (V16-02): the off-window p95 sum is the
			// direct outside-BH measure — never all-minus-bh percentile
			// subtraction (a constant workload reads outside=0 that way
			// while running all night), and never a median (a p50 hides
			// minority-of-window nightly activity — the #747 lesson).
			outside := r.Off
			if r.BH > ev.BHActivePeakMC {
				ev.BHActivePeakMC = r.BH
			}
			if outside > ev.OutsidePeakMC {
				ev.OutsidePeakMC = outside
			}
			if r.BH >= pol.BHActivityFloorMC {
				dayBHActive = true
			}
			if outside >= pol.OutsideBHIdleFloorMC {
				return nil // overnight activity breaks the signature
			}
		}
		if !covered {
			return nil
		}
		if dayApplicable && !dayBHActive {
			return nil // idle-always routes to the zombie rule, never here
		}
	}

	ev.WorkerNodes = workerNodes
	ev.ReclaimNodeHoursPerWeek = float64(workerNodes) * weeklyOutsideHours(sched)

	return &Advisory{
		HostedClusterID: hcID,
		Verdict:         SleepVerdict,
		Confidence:      "high",
		WindowStart:     windowStart,
		WindowEnd:       windowEnd,
		Details: map[string]any{
			"declared_tier":               tier,
			"bh_active_peak_cpu_mc":       ev.BHActivePeakMC,
			"outside_bh_peak_cpu_mc":      ev.OutsidePeakMC,
			"worker_node_count":           ev.WorkerNodes,
			"reclaim_node_hours_per_week": ev.ReclaimNodeHoursPerWeek,
		},
		Evidence: sleepEvidenceStrings(ev, sched),
	}
}

// weeklyOutsideHours is the non-business hours per week for the reclaim
// estimate. Windows crossing midnight (end <= start) wrap.
func weeklyOutsideHours(s bhschedule.Schedule) float64 {
	parse := func(hhmm string) int {
		var h, m int
		if _, err := fmt.Sscanf(hhmm, "%d:%d", &h, &m); err != nil {
			return 0 // unparseable window — treated as 0 minutes
		}
		return h*60 + m
	}
	window := parse(s.EndTime) - parse(s.StartTime)
	if window <= 0 {
		window += 24 * 60
	}
	return float64(len(s.Days)) * float64(24*60-window) / 60.0
}

func sleepEvidenceStrings(ev SleepEvidence, sched bhschedule.Schedule) []string {
	return []string{
		fmt.Sprintf("bh_active_peak_mc:%d", ev.BHActivePeakMC),
		fmt.Sprintf("outside_bh_peak_mc:%d", ev.OutsidePeakMC),
		fmt.Sprintf("bh_schedule:%s %s-%s %s", strings.Join(sched.Days, ","), sched.StartTime, sched.EndTime, sched.Timezone),
		fmt.Sprintf("worker_nodes:%d", ev.WorkerNodes),
		fmt.Sprintf("reclaim_node_hours_per_week:%.0f", ev.ReclaimNodeHoursPerWeek),
		"action:nodepool-scale-to-zero + pausedUntil (runbook); CP keeps running; wake ~= 20 min reprovision",
	}
}

// Advisory is one offline-computed advisory row, matching #717's serving
// contract (ADR-0349: typed details + raw evidence, additive-only).
type Advisory struct {
	HostedClusterID string         `json:"hosted_cluster_id"`
	Verdict         string         `json:"verdict"`
	Confidence      string         `json:"confidence"`
	WindowStart     time.Time      `json:"window_start"`
	WindowEnd       time.Time      `json:"window_end"`
	Details         map[string]any `json:"details"`
	Evidence        []string       `json:"evidence"`
}

// SortAdvisories orders output deterministically (by HC then verdict).
func SortAdvisories(a []Advisory) {
	sort.Slice(a, func(i, j int) bool {
		if a[i].HostedClusterID != a[j].HostedClusterID {
			return a[i].HostedClusterID < a[j].HostedClusterID
		}
		return a[i].Verdict < a[j].Verdict
	})
}
