# #846 — Populated Down-Migration Safety — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner go-ahead 2026-11-13: full class (option b) — fix 113-down + guards on
> {12, 28, 64, 125, 185, 189}; fail-loud policy with counts + remediation
> text; fix the stale roundtrip comment.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `43bc938a`. Tracker: #849 (V-BR7, P3).

## Problem (locked by the issue)

(a) 113-down restores the WRONG preceding contract — it destroys the
5-column term key that 64-up established and adds a 4-column constraint
that fails on populated multi-term data. (b) Narrowing down-migrations
recreate narrower unique constraints over rows the up-migration made
legal — PostgreSQL cannot create those constraints; the empty-schema
roundtrip green-lights a rollback that fails on populated data.

## History (recon-verified)

047 created `pvc_recommendation_sets` with an auto-named 4-col UNIQUE.
64-up (amended in `dcb0e414`) drops the three truncated name variants
and adds the 5-col term key. 113-up is an idempotent cleanup for
databases migrated before that amendment. The canonical post-64
contract is therefore the 5-col term key.

## The class (survey-verified)

| Down | Shape |
|---|---|
| 113 | wrong contract + narrowing — fix: restore the 5-col term key |
| 64 | drops `term` (creating duplicates) then adds the 4-col |
| 125 | quota_name-granular 4-col → 3-col |
| 185 | VM 6-col with schedule_type → 5-col |
| 189 | GPU org_id key → cluster-scoped key (already documented fail-loud) |
| 12 | rh_accounts UNIQUE(account) |
| 28 | UQ_Recommendation(workload_id, container_name) |

45/110/183/193 are symmetric recreates around NOT NULL-defaulted
column changes — safe, untouched.

## Mechanism (owner-approved)

- **113-down:** restore the post-64 contract — drop the 4-col variants
  and the term key, re-add the term key. The 4-col restoration belongs
  to 64-down in a full rollback. Automatically populated-safe.
- **Guards (12, 28, 64, 125, 185, 189):** a `DO` block FIRST in each
  down file — count collision groups on the constraint's columns,
  `RAISE EXCEPTION` with the migration number, the count, and
  deliberate-dedup remediation text. Fail-loud, never delete. Empty
  data passes through (roundtrip unaffected). **185 (owner decision
  2026-11-13):** the guard replaces the silent
  `DELETE ... WHERE schedule_type = 'business_hours'` — the message
  carries the exact deterministic cleanup statement and states that
  the schedule configuration is unaffected. Uniform policy, explicit
  consent; the auto-delete's atomicity argument was judged thin (the
  cleanup DELETE is idempotent).
- **Policy doc:** migrations/README.md gains a "Narrowing
  down-migrations" section.
- **Roundtrip comment:** fix the stale "reverses 044 through 026".

## Tests

Populated downgrade scenarios (package engine, testcontainers, one
container per scenario, skipped in short mode):

- **S1 (113 success):** up → seed multi-term PVCs → down to 112 →
  succeeds; the 5-col term key present.
- **S2 (64 guard):** continue S1's container → down to 63 → the 64
  guard raises with "000064 blocked".
- **S3 (125 guard):** seed multi-quota-name rows → down to 124 →
  "000125 blocked".
- **S4 (185 guard):** seed dual-schedule_type VM digests → down to
  184 → "000185 blocked".
- **S5 (189 guard):** seed cross-org GPU digests → down to 188 →
  "000189 blocked".
- **S6 (12/28 guards):** golang-migrate `Force` to the target version
  (the full chain below 25 is not exercised), seed duplicates, down one
  step → guard raises.

All scenarios are RED on the current tree: the unguarded downs either
raw-fail (Postgres duplicate-key error, no remediation text) or — for
113 — fail the contract assertion. Assertions match the guard MESSAGE
text so removing a guard cannot pass a scenario.

## Mutations

M1: revert 113-down → S1 fails. M2–M7: remove each guard → its
scenario fails (raw error or success without the message).

## Non-goals

#847 (offline attestation parity); #848 (weekly reclaim); #853 (EXISTS
verification). Symmetric-key downs (45/110/183/193) stay untouched.
