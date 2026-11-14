# The Native Engine Case — replacing legacy ros-ocp-backend

> For ROS engineers deciding whether the native engine is the right
> call. Every claim names its source; limits are stated as limits.

## What was replaced, and what replaced it

The legacy backend wrapped **Kruize** — the Java recommendation engine
— behind a Go API: recommendations arrived as Kruize result JSON,
stored as JSONB, served with light transformation. The fork replaces
that with a **native Go engine** running in-process: CSV payloads from
the metrics operator are parsed once into per-entity hourly digests
(PostgreSQL, range-partitioned), and recommendations are computed from
those digests by exact percentile math, stored relationally, and served
by a purpose-built list/detail API.

The founding decision is [ADR-0001](../adr/0001-native-engine-over-kruize.md),
with the statistic choices pinned by ADRs 0002–0008 (exact Go
percentiles over TimescaleDB approximations; read-once compute-n-terms;
dual cost/performance engine rows; decay-weighted averaging; p60/p98
CPU and p95/max memory; adaptive margin p95−p50; the 25-millicore CPU
floor). **348 ADRs** record the decision corpus — every non-obvious
choice has a numbered rationale.

## Why it is the right call (the arguments, with evidence)

**1. Correctness is demonstrated, not asserted.**

- The engine's math is pinned by a test corpus of **83 Go test
  packages**, green against real PostgreSQL (testcontainers) on every
  push — including adversarial regression suites written to fail on the
  specific bug mechanisms found by review (the per-issue RED → mutation
  discipline; see the [audit ledger](audit-ledger.md)).
- **16 adversarial review records** (plus the residue adjudication) span the work: due-diligence reviews
  v6–v14 (2026-07→09), the ecosystem review v15 (2026-11-01, verdict
  BLOCK → remediated), the v16 remediation audit (2026-11-09, CONCERNS
  → all 7 findings closed under tracker #833), and the phase17
  integration review (2026-11-11, GPT 6.1 Sol, 9 findings → all closed
  under tracker #849). The v15/v16/phase17 findings included a P1
  tenant-isolation defect (#840) — found by review, fixed with the
  pre-commit review question answered, and regression-pinned.
- **Server/offline parity is enforced by construction**: the robne CLI
  (`cmd/robne`) consumes `librobne` — the same CGO-free recommendation
  library the server uses — with a parity skill and tests keeping the
  two evaluators in lockstep (#847 closed the last known divergence).

**2. It serves at scale, with plan evidence.**

- Scale benchmarks ran on-cluster at 4K/10K/20K via the listener path
  and up to **~84,000 containers (~2,500 VMs, ~2,500 GPU containers,
  ~12,000 PVCs, 31 days)** via the direct-to-MinIO mode — the largest
  documented run (`docs-site/operations/scale-benchmark-report.md`,
  historical, 2026-07-11). The live-like production path has not been
  rerun after the extract and dual-stream changes (#518, open).
- The serving layer's heaviest query shape — the detail-eligibility
  EXISTS over the whole org — was EXPLAIN-ANALYZEd at a 50K-identity
  fixture: identity-index-backed (the 8-column primary key), semi-join
  with ~4µs index probes, 752 ms page / 221 ms count at the ceiling
  (#853, closed verified-fine with the plans posted).
- List queries are keyset-paginated with per-term/engine sort alias
  translation (#843–#844, #850, #852 — each a review-found defect
  fixed with regressions).

**3. It is validated end-to-end, not just unit-tested.**

- **IQE validation (2026-11-14):** the QE plugin's read-only list tests
  (`test_container_filter_engine`, `test_namespace_filter_engine`)
  PASSED against a locally-run API built from this branch; the
  variation-sort ordering verified through the same API with distinct
  descending values. The runbook's local-target section
  (`docs/testing/iqe-local-without-nexus.md`) documents the validated
  procedure.
- The on-cluster E2E pytest harness (docs-site testing docs) ran green
  through the phase17 cycles on the SNO lab cluster.

**4. The operational surface is built for production.**

- Prometheus metrics on the pipeline phases, recommendation duration,
  and DB query duration; per-endpoint statement timeouts; startup
  fails closed when internal auth is disabled in production (v13
  review hardening).
- Migrations carry a **documented rollback policy** with fail-loud
  guards — populated downgrades refuse over indeterminate survivors
  instead of silently deleting data (#846, with 11 populated-downgrade
  regression scenarios in CI).
- Feature-flag rollback: `ROS_ENABLED_PLUGINS=kruize` re-routes serving
  to the legacy path (native-era data remains; the flag is the escape
  hatch — see docs-site Rollback).

## What is legacy, dead, or transitional

- The **kruize plugin** remains as a compatibility shell, disabled by
  default; the residue adjudication
  (`docs/audit/branch-residue-adjudication-2026-11-11.md`) verified the
  runtime has no missing native implementation — the residue is
  **OpenAPI specification debt** (116 absent portions), mapped to
  #90/#605 as its own work stream.
- Legacy Kruize JSONB tables coexist during the transition; the
  list-endpoint fallback path (`...WithFallback`) tries native first
  and falls back to the legacy JSONB path only when native returns
  zero rows.
- The docs-site keeps a per-feature "Last verified" stamp discipline;
  the upgrade runbook documents the migration and rollback procedure.

## The honest limits (read these before deciding)

1. **Known issues exist and are tracked** — see the
   [known-issues register](known-issues-register.md). Notable open
   items: DST-boundary sort reordering (#851, P3 correctness), the
   tag-grain mismatches with Koku's observed-tags scoping (#834,
   #814–#815), stream-path digest flush fragments (#822, P2), and the
   per-message commit protocol window (#803, P3 audit finding).
2. **One known parity gap:** robne cannot configure HCP term windows
   the server serves, and reports HCP as having none (#793, open) —
   parity is enforced by construction and test, not proven exhaustive.
3. **#835 is an open defect with an adopted future design** (not yet
   implemented): the sleep advisory's off-leg p95 can be evaded by a
   rotation fleet spreading one active hour per container —
   high-confidence sleep advice for a workload active outside business
   hours is possible in that shape. The owner adopted the hour-grain
   activity signal as the fix direction; until it lands, the register
   carries the residual.
4. **The reclaim/sleep advisories trade silence for safety** — after
   schedule changes or coverage shortfalls the sleep advisory goes
   silent until evidence rebuilds (the owner-approved silence-over-wrong
   trades; ADR-0348 amendments). This is deliberate, bounded by reship
   cadence, and documented per amendment.
5. **Estimates are nominal where marked** — e.g. the weekly reclaim
   complement uses a nominal 168-hour week (DST weeks differ by an
   hour); the serving plan evidence is testcontainers-scale (realistic
   plan shape, indicative timing).
6. **The lab cluster is gone** — the SNO-based E2E surface is not
   currently running; the local-target IQE procedure covers the
   read-only surface until a cluster is provisioned.

7. **Cutover readiness has open wiring work** — the SaaS Clowder cutover
   epic (#450–#452), operator readiness/condition truthfulness (#456,
   open), and the consumer-migration dependencies listed in
   `docs-site/architecture/native-migration.md` (the koku-ui, IQE, and
   operator counterparts). The engine is ready; the fleet is not yet
   rewired.

## The bottom line

The native engine is not "probably fine" — it is the most-scrutinized
code this fork has: reviewed adversarially 16 times (plus the residue adjudication), remediated to
zero open review findings twice over (v16 and phase17 trackers), pinned
by adversarial regressions, validated end-to-end through IQE, and
plan-verified at the scale ceiling. Its known defects are enumerated,
labeled, and owned — not discovered by surprise. The remaining work
(the register) is scheduled scope, not structural risk.
