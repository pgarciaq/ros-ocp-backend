# #827 Sleep Evidence Coverage — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner approved the Gate-2 design 2026-11-10: **distinct observed hours per
> leg ≥ a floor fraction of the window** — the H-leg minimum-observation
> precedent (`HMinDataMinutes`), a methodological floor in the Policy struct
> (NOT a tenant settings field, per the #628 rule: floors aren't tenant
> knobs), defaulting to **half the window's hours**, unknown on shortfall.
> #835's hour-grain signal is NOT this — it concerns activity detection;
> this is observation coverage.

**Goal:** two observed hours per day must not count as complete sleep
evidence. The V16-03 repro: 28 observations (12:00 and 20:00 only) across 14
days produce high-confidence `schedule_nonprod_sleep` over 22 unobserved
hours per day.

**Mechanism:**
1. **Migration 000221**: `hcp_sleep_evidence` gains `bh_observed_hours` and
   `off_observed_hours` (SMALLINT, NULL = unknown → cold-start round 3,
   same accepted trade). Down: drop.
2. **Writer**: count DISTINCT hours (UTC hour-of-day of `IntervalStart`) per
   leg per container-day. The writer has the samples in hand — one pass.
3. **Server predicate**: `loadSleepEvidence` aggregates
   `MIN-per-container`… no — the workload-day aggregate needs the coverage
   that cannot hide a sibling's gap: **MIN over containers** of the leg's
   observed hours? No — the sparse defect is intra-day, and the
   partial-migration lesson (#826's F1) says: a fully-observed sibling must
   not mask an under-observed one. So: the workload-day's leg coverage =
   MIN over its container-rows of observed_hours (a single sparse container
   makes the leg under-covered → unknown). Query: add
   `MIN(bh_observed_hours)`, `MIN(off_observed_hours)` — hmm, but NULLs:
   MIN ignores NULLs — a legacy NULL-hours container would be skipped. Use
   the #826 rule again: coverage measured only when COUNT(hours) =
   COUNT(*); else unmeasured. So: `MIN(off_observed_hours)` with
   COUNT(off_observed_hours) = COUNT(*).
   - The floor: `SleepMinObservedHours` in Policy — but the two windows
     have different lengths (BH 8h, off 16h under a 9-17 schedule; weekends
     make BH 0h). A single "half the window" needs the window length…
     which the schedule defines. Simplest honest shape matching the H-leg
     precedent: a **fraction** policy field `SleepMinCoverageFrac`
     (default 0.5), compared against the leg's window hours for that day
     (computed from the schedule + dayApplicable, exactly what
     `weeklyOutsideHours` already derives from). Weekend/off-applicable
     days: BH window is 0 hours → the BH coverage requirement is vacuous
     (dayApplicable already handles it); off window = 24h → floor 12h.
4. **Offline**: `SleepSumsAll`… the offline legs are digest rows
   (container-day) — hours aren't in DigestRow. The CLI computes legs via
   `DailyDigestsWeighted`; coverage needs distinct hours per leg. The
   offline can compute hours directly from `csvLoaded.Rows` (interval_start
   distinct hours per window) — a parallel leg-hour count passed alongside
   the KeyedDigest slices. Shape: extend the advisory input with per-(day,
   ns, wl) observed-hour counts for both legs. The DB mode reads the new
   columns.
5. **Vendored librobne** carries it.

**Tests:**
- **RED**: the sparse fixture (2 samples/day: 12:00 BH-active, 20:00 off-
  quiet) through the raw-CSV path → must NOT fire (baseline: fires
  high-confidence with outside=0). Both server end-to-end and offline.
- **Controls**: the quiet raw-CSV control (24 hourly samples) still fires
  (it did in #826 — full observation); the night-burst repro still vetoes
  (activity AND coverage both work); the constant-active control; the
  existing seeded suite (seeder gains observed-hours seeding at full
  coverage — semantics preserved); the #761/#826 gates unchanged.
- **Boundary**: exactly-at-floor (8 of 16 off-hours observed → fires),
  one-below (7 of 16 → unknown). Distinct-hour semantics: 16 samples all
  within 2 hours → 2 distinct hours → unknown.
- **Mutations**: floor check removed → sparse fires; hours counted as
  samples (not distinct) → the distinct-hour boundary test fails; writer
  writes constant 24 → sparse fires. Stash-rerun.

**Docs:** ADR-0348 §5 amendment (the coverage contract: fraction floor,
distinct-hour semantics, MIN-over-containers, cold start); CHANGELOG.

**Non-goals:** no activity-detection change (#835, separately gated); no
schedule attestation (#831); no tenant settings field (the #628 rule); no
per-container coverage exposure in the API.
