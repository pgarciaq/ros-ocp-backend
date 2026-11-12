# #845 — Timezone-Shifted Sleep Applicability — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner go-ahead 2026-11-12: both recommendations accepted — the offline
> defensive `!sched.Enabled ||` form with a disabled-control test, and the
> server nil-schedule control test. Documented in docs-site/ADR if needed.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `45be931e`. Tracker: #849 (V-BR6, P2).

## Problem (locked by the issue)

The producer classifies timestamps in the schedule's LOCAL timezone; the
evaluator separately decides whether a UTC day requires BH evidence using
that UTC day's WEEKDAY (`bhschedule.DayAllowed(d.UTC().Weekday(), ...)`).
For timezone-shifted schedules the two disagree in both directions:

- **Kiritimati direction** (+14): Mon–Fri `00:00–08:00` local puts BH hours
  on Sun–Thu UTC. A Friday UTC has zero BH hours but `DayAllowed=true` →
  no BH row can exist → the #761 gate makes every user-workload day
  unknown forever — the advice never fires.
- **Reverse direction** (negative offsets): a UTC day carrying real BH
  hours can fall on a UTC weekday not in `Days` → `dayApplicable=false` →
  the BH-activity and completeness gates are wrongly skipped (lenient).

## Mechanism (owner-approved)

Derive applicability from the classified possible hours — the evaluator
already computes `bhPossible` per UTC day via `InBusinessHours` (the same
rule as the writer's leg split) — not a separate UTC weekday check:

- **Server** (`internal/engine/correlate/sleep.go`):
  `dayApplicable := sched == nil || bhPossible > 0`
  (the nil disjunct preserves the disabled→never-advise contract; nil
  schedules classify zero BH hours everywhere, so the BH-activity gate
  stays unsatisfiable → no advice).
- **Offline** (`librobne/advisory/sleep.go`):
  `dayApplicable := !sched.Enabled || bhPossible > 0`
  (defensive form: a disabled schedule is currently unreachable at eval —
  the caller requires non-empty BH legs, which a disabled schedule's leg
  split never produces — but the guard protects against future relaxation
  of that caller check).

`bhPossible` moves above `dayApplicable` (computed first). Equivalence
for UTC-aligned schedules (same-day and overnight): `DayAllowed(UTC
weekday) ⟺ bhPossible > 0` by the bucket-classification semantics — no
behavior change except timezone-shifted schedules.

Vendoring: librobne change requires `go mod vendor` +
`make vendor-librobne-check` (the server module builds the vendor copy).

## Tests

- **RED-1 (server, Kiritimati direction):** shifted schedule (+14 shape),
  fully observed window → must match (baseline: unknown forever).
- **RED-2 (server, reverse direction):** negative offset, a UTC day with
  BH hours on a non-allowed UTC weekday, BH activity below floor → must
  not match (baseline: lenient pass).
- **RED-3/RED-4:** offline twins of both.
- **Controls:** nil schedule (server) → no advice; disabled schedule
  (offline) → no advice; existing #760 weekend + #827 overnight tests
  stay green (UTC-aligned equivalence).
- **Mutations:** revert each site independently (each site's REDs fail);
  drop the nil/Enabled guard → the control tests fail.

## Review pass (gpt-6.1-terra, 2026-11-12) — 1 P1 + 2 P2, all verified and fixed

- **P1 — sub-hour windows:** a window contained in one clock hour
  (09:15–09:45, API-legal) classifies zero top-of-hour bucket starts —
  `bhPossible > 0` alone marked every day non-applicable → off-only
  idle evidence qualified → false sleep advice breaking the zombie
  partition. Fixed: `bhschedule.DayIntersectsBusinessHours` (minute-
  granular intersection, applicability only — the #827 floor stays
  bucket-based) as a fallback in both evaluators. Verified empirically
  (bucket enumeration). Controls: SubHourWindowOffOnlyIsSilent on both
  evaluators.
- **P2 — overnight equivalence claim false:** Mon–Fri 22:00–06:00 UTC
  gives Saturday six BH buckets from Friday's shift while
  DayAllowed(Saturday)=false — the pre-fix rule skipped the BH gates on
  real business hours (the same lenient-direction defect). Fixed: ADR +
  code comments corrected; OvernightSaturdayRequiresBH pins it on both
  evaluators (a direction-2 RED — fails on the reverted code).
- **P2 — coverage floor untested on newly-applicable days:** the
  reviewer verified that disabling the BH floor left every #845 test
  passing. Fixed: ShiftedDayCoverageFloorIsUnknown on both evaluators
  (a shifted applicable day with 2-of-8 observed BH hours → unknown).
- **Non-findings verified:** Kiritimati bucket enumeration matches the
  fixtures; fingerprints match FingerprintBounds; the direction-2
  fixture tests the activity gate (present-but-idle leg), not the
  missing-leg gate; the server writer emits NO rows when disabled
  (sleep_evidence.go `if !sched.Enabled { continue }`) — my original
  "off-only legs" comment corrected (the CSV path does produce
  off-only).
- **Mutations M1–M10 all caught:** M1/M2 (each site reverted to
  DayAllowed), M3/M4 (nil/disabled guards dropped), M5/M6
  (intersection fallback dropped), M7/M8 (DayAllowed revert — the
  overnight + shifted REDs), M9/M10 (BH coverage floor disabled).

## Non-goals

#851 (DST bucket-boundary reorder); #846–#848 (P3s); the eligibility
EXISTS verification.
