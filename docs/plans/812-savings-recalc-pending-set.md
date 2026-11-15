# #812 — Savings-Recalc Coalescing Guard: Pending Set of Clusters — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner go-ahead 2026-11-15 (`$properly-implement-feature #812`), following the
> readiness discussion: set-of-pending-clusters, **no org-wide subsumption**
> (both requests run; the redundancy is a cheap no-op, the subsumption logic
> isn't), recTypes union on same-cluster re-arrival.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`.
Tracker: #857 (serving P3 blitz, item 2).

## The verified mechanism

- `internal/engine/savings_recalc_guard.go`: the org-keyed flight stores a
  single `latestSavings` slot. A trigger arriving while `running` **overwrites**
  the slot. Flight running for A; B arrives (slot=B, pending); C arrives
  (slot=C) — **B is lost**. B's rate-cache entry and savings stay stale until
  some later trigger.
- Reachability confirmed on the producer side (koku recon 2026-11-15):
  `notify_ros_savings_recalculation` fires at the end of **every**
  `update_cost_model_costs` Celery task, per provider, immediately — no
  time-based batching anywhere in koku (the 6-hour beat jobs are tag/cost-group
  pushes, not savings callbacks). Koku's `WorkerCache` single-flight lock is
  keyed on `(schema, provider, dates)`, so it serializes same-provider
  duplicates but does NOT serialize **different providers** — a shared price
  list fans out N parallel tasks → N callbacks within seconds. The burst is
  real; the dangerous case is specifically different clusters, which is what
  the set fixes.
- koku always sends all 5 recTypes (`DEFAULT_RECOMMENDATION_TYPES`), so
  same-cluster unions will merge identical lists in practice; the union logic
  is defensive depth, not a hot path.

## Mechanism (the fix)

`recalcFlight.latestSavings` (single slot) becomes `pending
map[string]*savingsRecalcParams` keyed by clusterUUID (the raw string; `""`
= org-wide is its own key):

- **Arrival:** under the mutex, merge into the set — same key unions recTypes
  (via `normalizeSavingsRecTypes` semantics: trim/lower/dedupe/validate),
  new key inserts a copied entry. If `running`, count the coalesced metric
  and return; else set `running` and enter the drain loop.
- **Drain:** loop — under the mutex, if the set is empty, clear `running` and
  exit; else pop the **lexicographically smallest key** (deterministic drain
  order; each entry is independent so order only affects which runs first by
  milliseconds), release the mutex, run
  `RecalculateSavingsForOrg` + the three `InvalidateOrg` calls, re-check.
- **No lost wakeups:** the emptiness check and `running=false` share one
  mutex hold; an arrival after that sees `running==false` and starts its own
  flight.
- **Same key arriving while its own entry is being processed** (already
  popped): becomes a fresh pending entry → re-run. Correct — it is a new
  trigger and may carry new recTypes.
- **No org-wide subsumption:** an org-wide (`""`) entry and specific-cluster
  entries coexist in the set; each runs its own recalc. The redundant
  specific recalc right after an org-wide that covered it recomputes the
  same values — a harmless no-op.
- The metric (`rosocp_savings_recalc_coalesced_total`) still counts every
  arrival during a flight, including same-key merges.

**Not changed:** one flight at a time (the guard's purpose — no unbounded
spawning); the burst coalescing (N same-key arrivals → one trailing run);
the invalidation points (after each recalc, as today); ADR-0118's
invalidate-twice pattern outside the guard.

## Tests

1. **The A/B/C regression** (the issue's acceptance, evolving
   `TestTriggerSavingsRecalcCoalesced_UsesLatestParameters`, which pins the
   bug today — it asserts C overwrites B): flight running for A; B arrives
   `["container"]`; C arrives `["node"]`; on completion **A, B, and C each
   recalculated** with their own recTypes (captured per-cluster via
   `SetClusterSavingsRecalcFuncForTest`), B not overwritten.
   **RED on baseline:** B never appears in the captured calls.
2. **Same-cluster merge:** A running; B arrives `["container"]`; B arrives
   `["node"]` → B runs **once** with both types (the coalescing/dedupe
   criterion). **RED on baseline:** B runs once with `["node"]` only.
3. **Arrival during a trailing run** (added by the fresh-context review):
   D arrives from inside B's trailing run → drained by the next loop pass.
   Passes on baseline (guards the loop re-check both implementations have);
   catches the snapshot-and-clear shortcut (M3).
4. **Same-cluster re-arrival during its own run** (added by the review):
   B re-arrives while B's popped entry is executing → a fresh pending entry
   → B runs twice, once per arrival. Passes on baseline (regression guard).
5. **Org-wide does not subsume specifics** (added by the review): an
   org-wide (`""`) arrival and a specific-cluster C arrival during a flight
   BOTH drain (3 runs). **RED on baseline** — there the org-wide entry is
   the victim of C's overwrite (a third angle on the bug).
6. **Existing tests hold** (the "coalescing benefit preserved" criterion):
   `CoalescesOverlappingJobs` (5 rapid same-key triggers → at most 2 runs)
   and `CoalescedMetricIncrements` pass unchanged — same-key arrivals still
   coalesce into one entry.

## Mutations run

- **M1 (revert to single-slot):** the RED itself — both tests 1 and 2 fail
  with messages naming the mechanism (B dropped by C's arrival; `container`
  missing from the union).
- **M2 (drop the same-cluster union, keep latest types):** test 2 fails
  (`ElementsMatch` — `container` missing); test 1 still passes — the tests
  discriminate between "set" and "union".
- **M3 (snapshot-and-clear, no re-check):** tests 1, 2, and 3 all fail
  (arrivals after the snapshot lost); the single-trigger cache test
  correctly still passes.

## Scope notes (flagged, not fixed here)

- `internal/reship/trigger_guard.go` has the same latent shape
  (`latestClusterUUIDs` batch overwritten by a later arrival). The
  fresh-context review confirmed the reachability condition IS met
  (concurrent BH settings edits at different scopes — cluster-level X + Y,
  or cluster + org-level — pass different cluster sets for the same org;
  `reshipClusterIDs` resolves org-level to all org clusters, cluster-level
  to just that cluster) but also verified the recovery path the savings
  guard lacks: `MarkReshipPending` durably flags the clusters BEFORE
  `TriggerAsync`, cleared only on masu success, and the reship poller
  re-drives flagged clusters — a displaced cluster is delayed, not lost.
  Filed as a separate P3 issue; out of #812's scope.
- `internal/engine/threshold_recalc_guard.go` is **safe** (verified by the
  review): flight keyed by `orgID+recType`, the pending slot carries no
  parameters (boolean flag only) — nothing to lose.

## Deliverables

- Fix + tests in `internal/engine/savings_recalc_guard.go` /
  `savings_recalc_guard_test.go`.
- ADR-0125 dated clarification (the savings guard's granularity: latest-slot
  → pending set; ADR-0125 is the pattern's origin, the savings guard cites
  it in its header comment).
- CHANGELOG entry.
- docs-site corrections (found by the review): adversarial-reviews.md #11
  and #62 dated updates (the savings guard no longer uses latest-parameters
  semantics); configurability.md — two pre-existing metric label errors
  fixed (`ros_savings_recalculation_total` has no `org_id` label;
  `rosocp_savings_recalc_coalesced_total` is unlabeled) + Last verified
  bump.
- This plan doc; completion report per properly-implement-feature.
