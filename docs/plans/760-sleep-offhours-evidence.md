# #760 Sleep Off-Hours Evidence (A′) — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner approved the **A′ design** 2026-11-08 (purpose-built sleep-evidence table;
> performance-paramount constraint; B stopgap not needed; C rejected by the issue).
> Implementation in ros-ocp-backend (server ingestion + correlate, offline librobne,
> the vendored copy).

**Goal:** the sleep predicate's outside-BH safety leg reads **direct off-hours
evidence** — no percentile subtraction. A constant 300m workload (all-hours p50=300,
BH p50=300 → outside=0 today) must not receive safe-sleep advice; its off-hours
p50 IS 300 and the veto must fire.

**Design (A′):** a thin `hcp_sleep_evidence` table, per (org, cluster, ns,
workload, workload_type, container, bucket_date): `bh_cpu_p50_mc` and
`off_cpu_p50_mc`, both nullable. Written during ingestion, only when a schedule is
enabled for the namespace, **from the all-hours groups' complete samples** (the
staged samples are raw and lossless — #758): split each container-day's samples
by `InBusinessHours`, compute each side's p50 through the canonical
`ComputeContainerDigestWeighted` (nil weight — the split IS the filter), queue a
replacement upsert on the same batch sender. Per-container grain (NOT per-workload)
is required: the staged path flushes in key chunks (a workload's containers can
split across chunks) and incremental sums would break replacement-write
idempotency (replayed manifests must converge).

**Why per-container thin rows still beat a third digest stream (A):** ~2 payload
columns vs ~20+, a side table (not the hot digest table), zero blast radius on
the unfiltered digest readers (zombie ×2, C-leg — no `schedule_type` pin needed),
no prune-of-BH-digests entanglement beyond a symmetric delete.

**Predicate rewrite (both layers):** `loadSleepSignature`/`DaySums` replaced by
evidence-shaped loads: per (day, ns, wl): `SUM(bh_p50)`, `COUNT(bh_p50)`,
`SUM(off_p50)`, `COUNT(off_p50)`. `outside = off sum` — direct, no subtraction;
`bhPeak`/`outsidePeak` from the sums. #761's workload-grain gate carries over:
a user workload with either count = 0 reads unknown. The `covered` flag: any
workload with both legs measured. The all-hours digest stream becomes irrelevant
to sleep (it existed only for the subtraction). **BH-leg refinement (documented,
safe direction):** the evidence BH leg is the pure in-window p50 — today's BH
digest with `off_hours_weight>0` blends off-hours samples at fractional weight;
the evidence table's legs are exact complements.

**Fan-out (verified — all sites):** every digest-compute path funnels through
`upsertContainerDigestsOnSender` (pipeline `upsertContainerDigests`, single-tx
`commitIngestInSingleTx`, staged `finalizeStagedGroups` → `upsertContainerDigests`)
— ONE hook. GPU/node/namespace paths untouched (sleep reads container grain).
Prune: `bhschedule.PruneClusterBusinessHoursDigests` and siblings gain the
symmetric evidence delete (schedule removed → stale evidence gone).

**Offline (robne):** compute both legs via `csv.DailyDigestsWeighted` with 0/1
window filters (canonical builders); `DaySums(all, bh)` → `SleepSums(bh, off)`
(`DaySum.All` → `Off`); `SleepCandidate` semantics updated in place.

**Migration 000219:** unpartitioned (thin rows, scheduled scopes only — the
`hcp_correlation_advisories` precedent; volume reasoning in the comment); PK =
the full container-day key; `latestMigrationVersion` → 219.

**Tests:**

- **RED both layers (the repro, against the CURRENT predicate):** constant 300m
  × 14 days incl. nights, 7-day 09:00–17:00 schedule. Server: digest rows
  (all=300, bh=300 — what the canonical builders yield) → cycle → assert silent;
  baseline fires with outside=0. Offline: `DaySums` all=300/bh=300 → nil;
  baseline returns the advisory. Post-fix the tests transition to the evidence
  shape (bh=300/off=300 → veto), with the writer test proving raw hourly rows →
  evidence rows through the canonical machinery.
- **Writer tests:** real hourly rows, distinct in/out values (100 in-window /
  900 off — filter-inversion must NOT be invisible), window-edge samples
  (08:59/09:00/17:00/17:01), schedule-disabled (no rows), replay idempotency
  (re-ingest converges), chunk-splitting (a workload's containers across staged
  chunks).
- **Predicate controls:** genuine BH-only (300 in, ~10 off) still fires;
  constant-overnight silent; partial coverage (schedule enabled mid-window →
  earlier days have no evidence → unknown); #761 gate (off-only container →
  workload bhMeasured=false → unknown); fan-out (150+150 = 300 → veto; a single
  300 → veto — sum semantics); no-evidence cluster → silence (the cold start).
- **Mutations:** predicate ignores the off leg → repro fires; writer's off
  filter inverted → the 100/900 control fires wrongly; stash-rerun.

**Docs:** ADR-0348 §5 amendment (the formula: direct off-hours evidence, no
subtraction; the pure-complement legs; the #761 gate carried; the BH-leg
asymmetry note); CHANGELOG; docs-site check for any published statement of the
subtraction formula (robne-cli.md / hosted-control-plane.md) and update if found.

**Non-goals:** no third digest stream; no zombie/C-leg change (untouched —
verified); no threshold changes (`SleepOutsideBHIdleFloorMC` semantics now
measured against a true off-hours p50 — the calibration intent finally honored);
no serving-contract change (`outside_bh_peak_cpu_mc` keeps its name, now honest);
no namespace/GPU/node evidence.
