# #763 API-Tax Farm Identity — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner approved **Option A** 2026-11-07: extend the `hcp_correlation_advisories`
> key with `management_cluster_uuid`. Implementation in ros-ocp-backend.

**Goal:** two management clusters (farms) in one org/window each get their own
api-tax advisory row — a farm's identity (`management_cluster_uuid`) is never
served with another farm's evidence.

**Mechanism (verified at HEAD, zero drift since the 3061a76f baseline):** the
table PK is `(org_id, hc_cluster_id, window_start)`; api-tax rows write
`hc_cluster_id=''` (shared-plane evidence, ADR-0349 §3), so two farms in one
org/window hit the same key. The `DO UPDATE` replaces the evidence fields
(verdict, confidence, p99, n_signals, expires_at) but not
`management_cluster_uuid` — one row: the first farm's UUID with the second
farm's webhook facts. The #791 scope filter matches `management_cluster_uuid`,
so a farm's grant can see another farm's evidence.

**Fix (Option A):**

1. **Migration 000218** (`hcp_advisories_mgmt_pk`): drop and recreate the PK as
   `(org_id, hc_cluster_id, window_start, management_cluster_uuid)`. The table
   is tiny, unpartitioned, self-cleaning by expiry. The down migration
   collapses per-farm rows back to one per old key (greatest
   `(expires_at, management_cluster_uuid)` wins) before restoring the narrower
   PK — a rollback must not fail on duplicate keys.
2. **All four writers' conflict targets** extended with
   `management_cluster_uuid`: `job.go` writeAdvisory, `apitax.go`
   writeAPITaxAdvisory, `zombie.go`, `sleep.go`. `DO UPDATE SET` unchanged —
   the conflicting row already carries the same identity under the extended
   target; identity fields stay unreplaced by design.
3. **Test-side conflict targets** follow (they would error against the new PK):
   `handlers_hcp_advisories_integration_test.go` lines 42 (seed helper) and 197
   (the ADR-0349 §3 upsert-invariant pin — same (org, HC, mgmt) key, semantics
   unchanged).
4. `latestMigrationVersion` → 218.

**Why the per-HC invariant survives:** for per-HC rows the management cluster
is functionally determined by the HC (one farm owns an HC), so
at-most-one-advisory-per-(HC, window) holds semantically exactly as before —
ADR-0349 §3's own framing. For api-tax rows (`hc=''`) the mgmt UUID now
distinguishes farms — two rows, each correctly attributed. The #791 scope
filter improves: each farm's grant sees only its own advisory.

**Tests:**

- **RED (end-to-end):** `TestAPITax_TwoFarmsDoNotMix` — two farms seeded in one
  org (webhooks `slow.a.example` / `slow.b.example`, distinct hot p99s), one
  `RunAPITaxCycle` → assert fired ≥ 2 and exactly two rows, each farm's UUID
  carrying its own webhook in n_signals. Baseline: one row — the first farm's
  UUID with the second farm's evidence (the audit's proof shape).
- **Writer-level controls:** order reversal (B before A — still two attributed
  rows; the baseline's UPDATE-not-identity asymmetry makes order matter) and
  repeated same-farm writes (one row, evidence refreshed in place — the upsert
  contract preserved).
- **Per-HC invariant:** the existing correlate suite (headroom/zombie/sleep
  write per-HC rows through the extended target) stays green.
- **Scope improvement (#791):** `TestReaderScope_HCPAdvisories_SharedPlaneFarmIsolation`
  — two farms' shared-plane (`hc=''`) rows; an A-only grant sees exactly A's
  row, the B grant sees B's, an unrelated grant sees neither.
- **Mutation/stash check:** the new tests must fail on the stashed original
  implementation (writers + migration reverted) with the mixed-row shape; the
  failure message names the collapse.

**Docs:** ADR-0349 amendment (Option A, the multi-farm reasoning, the
escape-hatch clause now exercised); CHANGELOG entry.

**Non-goals:** no verdict-column PK extension (a different hazard, C-variant);
no changes to the serving reader (it already lists rows; the scope filter
already matches mgmt); no seeding/scheduling changes; no threshold changes.
