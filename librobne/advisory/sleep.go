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
}

// DefaultSleepPolicy mirrors the server's compiled defaults
// (DefaultPolicy in internal/engine/correlate).
func DefaultSleepPolicy() SleepPolicy {
	return SleepPolicy{WindowDays: 14, BHActivityFloorMC: 10, OutsideBHIdleFloorMC: 10}
}

// DayKey identifies one (namespace, workload) pair on one day.
type DayKey struct {
	NS string
	WL string
}

// DaySum is one (day, namespace, workload) pair across the two digest
// streams. A day counts as covered only when BOTH streams are measured —
// partial business-hours coverage reads unknown, never zero-activity
// (ADR-0348 §5).
type DaySum struct {
	BH          int64
	All         int64
	BHMeasured  bool
	AllMeasured bool
}

// DaySums groups the two digest streams into per-(day, ns, workload) CPU
// sums (millicores, the p50 column — the server's signature input).
func DaySums(all, bh []types.KeyedDigest) map[string]map[DayKey]DaySum {
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
				d.All += kd.Row.CPUUsageP50MC
				d.AllMeasured = true
			}
			out[day][k] = d
		}
	}
	add(all, false)
	add(bh, true)
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

// SleepCandidate evaluates one hosted cluster. Returns nil when the HC does
// not qualify (undeclared, no schedule, unknown days, or the signature
// breaks) — silence, mirroring the server's never-fail-on-evidence posture.
func SleepCandidate(hcID, tier string, sums map[string]map[DayKey]DaySum, sched bhschedule.Schedule, pol SleepPolicy, workerNodes int, syncedExact map[string]bool, syncedPrefixes []string, now time.Time) *Advisory {
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
		covered := false
		dayBHActive := false
		for k, r := range rows {
			if !r.BHMeasured || !r.AllMeasured {
				continue
			}
			covered = true
			if !isUserNamespace(k.NS, exact, prefixes) {
				continue
			}
			outside := r.All - r.BH
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
		if !dayBHActive {
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
