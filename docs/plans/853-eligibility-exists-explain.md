# #853 — Eligibility EXISTS EXPLAIN Verification — Implementation Plan

> **For agentic workers:** this is a VERIFICATION task — the deliverable
> is evidence and a disposition, not a fix. Record, do not tune (the
> issue's constraint). Owner decisions 2026-11-13/14: fixture at the
> benchmarked scale ceiling (~50K container identities); the structural
> guard asserts index usage (never timing); disposition decided FROM
> THE NUMBERS (single-digit ms at 50K → verified-fine; tens of ms or
> per-row sequential → a separate index follow-up).

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `60439f9f`. Tracked as #853 (from #842's asserted-claim row).

## What is verified

The #842 eligibility mechanism's correlated EXISTS, at org scale, with
the ACTUAL serving-layer SQL:

- Container: `applyNativeContainerDetailEligibility` —
  `EXISTS (SELECT 1 FROM recommendation_sets rs WHERE rs.org_id = …
  AND 6 identity columns … AND detail predicates)` on
  `org_container_keys`.
- Namespace: `applyNativeNSDetailEligibility` — the twin on
  `namespace_recommendation_sets`.

Runs when detail predicates exist — the default request (date range)
counts — for BOTH the page and the count query.

## Recon findings (2026-11-13)

- **The container identity index is a 5-of-6 PREFIX** (000062:
  `(org_id, cluster_uuid, namespace, workload, container_name)` — no
  `workload_type`): the #842 "identity-index-backed, review-verified"
  claim was loose. Each probe: prefix index scan + workload_type heap
  filter. The EXPLAIN decides whether that's fine.
- **The namespace side is properly covered** — its 3-column identity
  is the leading prefix of `idx_ns_recs_native_key`.
- Scale grounding: the scale-benchmark skill's benchmarked range
  (4K–50K+ workloads) → the fixture seeds ~50K container identities
  (plus ~5K namespaces, 2–4 rs rows per identity for term/engine).

## Mechanics

A Go test in `internal/model` (short-mode skipped, runs in CI):

1. Seed the org-scale fixture (batched inserts), then `ANALYZE`.
2. Capture the actual SQL via a capturing gorm logger on the real list
   functions — 8 shapes: both entities × (default date-range request,
   explicit filter) × (page, count).
3. `EXPLAIN (ANALYZE, BUFFERS)` each captured query with its bound
   args; dump the plans.
4. Assert only the STRUCTURAL guard: the EXISTS inner side uses an
   index (never a Seq Scan on the inner). Timing recorded, never
   asserted.
5. Guard honesty check: drop the index the plan uses (in the test DB)
   → the guard must fail (the test discriminates a real plan change).

## Evidence and disposition

Plans + per-node estimates-vs-actuals + timings posted to #853 with
the disposition: verified-fine (with the testcontainers realism
caveat: realistic plan SHAPE, not production timing) or a separate
optimization issue with the numbers — any index change is the owner's
decision.

## Non-goals

Tuning, index changes, thresholds (the issue's constraint); #854 (the
hand-off package — this verification's disposition feeds its
known-issues register).
