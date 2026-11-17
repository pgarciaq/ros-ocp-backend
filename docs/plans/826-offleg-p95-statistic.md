# #826 Off-Leg p95 Statistic — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner approved 2026-11-10: the off-leg statistic moves p50 → **p95** by house
> convention (#747 — the zombie rule's activity measure made the identical
> migration for the identical reason: "the p50 reads partial-day activity as
> idle"); p99 is reserved for latency SLIs (H leg, api-tax); max gates nothing.
> #827 (coverage) is the NEXT cycle — deliberately not conflated.

**Goal:** sustained off-hours activity must veto the sleep advisory. Both V16
reproductions — night-burst (2000m for 6 of 16 off-hours; p50 = 0) and
alternating containers (each of two containers active 7 of 16 off-hours;
per-container p50 = 0, workload active 14 of 16) — must read as activity, not
silence.

**Mechanism:**
1. **Migration 000220**: `hcp_sleep_evidence` gains `off_cpu_p95_mc BIGINT`
   (NULL = unknown — existing rows read offMeasured=false → the #761 gate →
   silence until re-ingest: cold-start round 2, the #760-accepted trade,
   restated in the migration comment). Down: drop the column.
2. **Writer** (`internal/ingestion/sleep_evidence.go`): compute the off leg's
   p95 via the same digest core (`ComputeDigest(values).P95` — no new math);
   keep writing `off_cpu_p50_mc` (continuity; the BH leg keeps p50 — its
   failure direction, underestimating activity, suppresses advice, which is
   safe; documented).
3. **Server predicate** (`internal/engine/correlate/sleep.go`):
   `loadSleepEvidence` reads `SUM(off_cpu_p95_mc), COUNT(off_cpu_p95_mc)` —
   the value AND the measured flag key on the p95 column. Workload grain
   stays SUM (sum of per-container p95s is a conservative upper bound for
   the veto direction — each p95 bounds its container's activity; summing
   bounds the workload's; documented).
4. **Offline** (`librobne/advisory/sleep.go`): `SleepSums`'s off leg consumes
   `kd.Row.CPUUsageP95MC` (the BH leg keeps P50) — the CLI's
   `DailyDigestsWeighted` legs already carry the full DigestRow, so no CLI
   change; `pgdigest.Read/WriteSleepEvidence` carry the new column for DB
   mode. DaySum docs updated: Off is the p95 sum.
5. No threshold change: p95 ≥ p50 always, so the veto is strictly more
   sensitive against the same `SleepOutsideBHIdleFloorMC` — safe direction;
   the floor remains the sensitivity knob. Sparse-coverage (#827) stays
   firing — its off-p95 is a genuinely observed 0 — scope separation holds.

**Tests:**
- **RED (end-to-end, raw CSV through the canonical path)**: night-burst and
  alternating-containers as permanent regressions in
  `internal/engine/correlate/sleep_test.go` (ParseAndDigestCSV →
  RunSleepCandidateCycle → assert NO advisory). Baseline: fires
  high-confidence with outside=0 — the V16 proof.
- **RED (offline)**: off legs carrying P50=0/P95=2000 → `SleepCandidate` nil.
  Baseline (consumes P50): advisory.
- **Predicate-grain adversarial test** (seeded, distinct values): p50=0 but
  p95=200 on the off leg → veto — pins that the predicate reads the p95
  column, not the p50 (same-value seeding cannot mask it).
- **Controls**: constant-active (300m always) still vetoed (off-p95=300);
  fully-covered-quiet still fires (off-p95=0); the existing seeded suite
  passes with the seeder writing `off` to both columns (semantics
  preserved); sparse-coverage still fires (that is #827's, not regression).
- **Mutations**: predicate re-reads p50 → repros fire; writer computes p50
  into the p95 column → repros fire; offline SleepSums reads P50 → offline
  repro fires. Stash-rerun of the full set.

**Docs:** ADR-0348 §5 amendment (the p95 statistic, the #747 convention
rationale, sum-of-p95s conservatism, BH-leg-stays-p50, cold-start round 2);
CHANGELOG Fixed entry.

**Non-goals:** no coverage columns or thresholds (#827, next cycle); no
schedule attestation (#831, separately gated); no BH-leg statistic change;
no floor recalibration; no new policy knob.
