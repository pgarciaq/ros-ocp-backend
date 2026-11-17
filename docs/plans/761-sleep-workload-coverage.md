# #761 Sleep Workload Coverage — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Design presented and proceeded with 2026-11-07 (the issue's scope gate
> pre-authorizes the conservative missing-workload coverage gate; the unknown
> semantics follow ADR-0348 §5's "unknown, never zero-activity" philosophy).
> Implementation in ros-ocp-backend (server correlate + offline librobne +
> the vendored copy).

**Goal:** a user workload with exactly one digest stream (the audit's
night-only CronJob: `All:2000, AllMeasured:true, BHMeasured:false`) makes the
day **unknown** — never silently skipped while a sibling workload establishes
the day's coverage — so `schedule_nonprod_sleep` cannot fire for a cluster
with a night-only user workload.

**Mechanism (verified at HEAD):** both predicates `continue` over a workload
missing either stream BEFORE the namespace check and the outside computation
(server `evalSleepSignature` sleep.go:187-191; offline `SleepCandidate`
librobne/advisory/sleep.go:155-159). The missing BH row is a NORMAL producer
shape: `GroupCSVRowsForStream` drops weight-≤0 rows for the BH stream (the
`off_hours_weight=0` fast path; weight is time-based, not usage-based — a
workload present-but-idle during BH still gets a zero-valued BH row, so
BH-row-absent ⟺ zero rows during BH ⟺ night-only or silent-in-BH).

**Fix (the per-user-workload unknown gate):**

1. In both loops, move the user-namespace check BEFORE the stream check
   (system workloads' stream shape is irrelevant — exclusion preserved;
   `covered` semantics unchanged), and a USER workload with exactly one
   stream measured returns unknown (server: `match=false, known=false`;
   offline: `nil`). Not a veto — the off-hours magnitude is unknowable
   (the scope gate's demand), and both routes are silence on the write path.
2. Symmetric: BH-row-without-all-hours also reads unknown (structurally
   near-impossible from this producer — both streams derive from the same
   CSV rows; the gate handles it conservatively for free).
3. No valid sleep candidates are lost: BH-absent ⟺ zero BH-hours rows ⟺
   night-only or silent-during-BH — neither belongs in the signature. This
   reasoning and the workload-grain coverage rule go into an ADR-0348 §5
   amendment.
4. Vendor sync for librobne (`go mod vendor` + drift check — the #762 lesson).

**Tests:**

- **RED server** (`internal/engine/correlate/sleep_test.go`): the seeder's
  `sleepDay` gains `bhAbsent bool`; the audit fixture — web workload
  (both streams, BH=300, outside=0) + `{"batch-jobs","night-cron"}` with
  `bhAbsent, all:2000`, 14 days → `RunSleepCandidateCycle` writes nothing.
  Baseline: fires high-confidence.
- **RED offline** (`librobne/advisory/sleep_test.go`): the same shape via
  `DaySums(all, bh)` where the night job appears only in `all` →
  `SleepCandidate` returns nil. Baseline: advisory.
- **Producer normal-shape proof** (`internal/ingestion/digest_test.go`): a
  container whose every row is off-hours produces NO BH group (the existing
  family pins mixed rows only — this pins the all-off-hours shape).
- **Controls**: system-namespace workload with a missing BH row stays
  skipped (sleep still fires); the night job ALONE (no sibling) is unknown
  on both baseline and fix (pin — the day-level `covered` check, distinct
  mechanism); BH-only workload (reverse shape) reads unknown after the fix;
  valid fully-covered candidates still fire (existing suite); distinct
  values (web 300 vs night 2000) so mis-mapping cannot pass.
- **Mutations**: the gate removed in either layer → the REDs fire again;
  stash-rerun for the original-implementation contrast.

**Docs:** ADR-0348 §5 amendment (workload-grain coverage; why no valid
candidates are lost; the symmetric case); CHANGELOG entry.

**Non-goals:** no zombie-rule change (verified single-stream — no hole); no
producer change (the omission is by design); no threshold changes; no
serving-contract change (the verdict's silence is the fix's observable).
