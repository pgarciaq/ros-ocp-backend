# #790 Cluster Filter by Alias — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `filter[cluster]=<alias>` and `filter[cluster]=<uuid>` both return 200 with correctly-scoped rows on nodes, pvcs, snapshots, quota, and cluster-quota — with unknown aliases yielding 200+empty, Koku's contract.

**Architecture:** One shared predicate helper — UUID value binds exactly against the table's `cluster_uuid` column; alias value resolves through the same `clusters`-table subquery the seven working endpoints use (`parseClusterParams`, `utils.go:337`). Applied at the five raw-SQL sites, composing with the #752 server-scope clauses already in place.

**Tech Stack:** Go, echo, pgx raw SQL, testcontainers, testify; the #752/#786 test patterns (`permsApp`, seeded two-cluster orgs).

**Spec:** [GH #790](https://github.com/pgarciaq/ros-ocp-backend/issues/790) + owner decisions 2026-11-03:
1. **Unknown alias → 200 + empty** — matching Koku, verified at `koku/koku/api/report/all/openshift/provider_map.py:51-54` (`filter[cluster]` → `icontains` on `cluster_alias` OR `cluster_id`, an ORM WHERE clause; unknown value matches nothing). Also matches the seven working ROS endpoints. The issue's 400-vs-404 question resolves to *neither*.
2. **The "Cluster names shows UUID" symptom is dismissed** — the integration (KMO config / Sources) forgot to set the cluster name; `cluster_alias` is empty and the UUID is the correct fallback (our own heatmap SQL coalesces `cluster_alias → cluster_uuid` for this case). Not a code bug; recorded as an issue comment, not a body edit.
3. **Symptom 1 (the `ANY(...)` branch divergence) is stale** — the cited clauses at quota `:182`/crq `:171` are now #752's server-scope clauses (server-resolved UUIDs, never aliases). Corrected in the close-out.

**Recon-verified facts this plan argues from:**

- The five sites bind the raw filter value against a `uuid` column; a non-UUID alias reaches Postgres → `SQLSTATE 22P02` → 503. Confirmed at current HEAD: `handlers_node_utilization.go:246` (`AND nr.cluster_uuid = $n`), `handlers_pvc_filters.go:56`, `handlers_quota_recs.go:164`, `handlers_cluster_quota_recs.go:156`, `handlers_snapshot.go:135`.
- The working endpoints' alias mechanism (`parseClusterParams`, `utils.go:330-340`): UUID → exact bind on the uuid column; non-UUID → `IN (SELECT cluster_uuid FROM clusters WHERE cluster_alias = ?)` — no org predicate in the subquery (UUIDs globally unique; the outer query stays org-scoped). Unknown alias → no rows → 200+empty.
- All five endpoints take a **single-value** client filter (`queryparams.FirstFilter`); multi-value client cluster filters are not supported on them today — out of scope, noted.
- The #752 scope clauses (`cluster_uuid::text = ANY($n)` with server-resolved UUIDs) sit in the same filterSQL chains on quota/CRQ/node-utilization — the fix must compose (AND), not replace.
- The five endpoints' responses carry only `cluster_uuid` (no alias field) — consistent with the dismissed display symptom.

## Global Constraints

- Production change across 5 handler files + 1 new helper — commits wait for explicit owner go-ahead (repo approval gate).
- **No error-semantics change**: unknown alias → 200+empty; no new 4xx anywhere.
- The #752 server-scope behavior is preserved exactly: A-only grant + alias-of-B filter → empty (the client filter resolves B; the server scope ANDs A-only; the intersection is empty).
- No drive-by refactors; the helper is the mechanism, the five call sites are one-line-shaped changes each.
- No `t.Skip`, no weakened assertions; rules-out comments per adversarial-fixtures.md; `t.Cleanup(config.ResetForTest)` at every RBAC-affecting Setenv site.
- Push target: fork (`pgarciaq`), branch `pgarciaq-rosocp-superpowers-phase17`.

## Review Focus

1. **The 22P02 mechanism** — the regression must fail pre-fix with the 503 (not merely a wrong count), proving it exercises the cast failure. → Task 2 RED assertions.
2. **Filter/scope composition** — alias-of-B + A-only RBAC grant → 200 + empty (client filter composes with, never bypasses, the server scope). → Task 2 composition test.
3. **Unknown alias** → 200 + count 0 on every endpoint (Koku contract), never 503, never 4xx. → Task 2.
4. **UUID form unchanged** — the already-working UUID filter keeps its exact behavior (positive control). → Task 2.
5. **Shared, not copy-pasted** — one helper, five call sites; no per-handler resolution logic. → Task 3 + review.

---

### Task 1: The shared predicate helper (self-contained TDD)

**Files:**
- Create: `internal/api/cluster_filter_predicate.go`
- Test: `internal/api/cluster_filter_predicate_test.go` (package `api` — the helper is unexported)

**Interfaces:**
- Produces: `clusterFilterPredicate(column, value string, argIdx int) (sqlFragment string, args []any, nextIdx int, ok bool)` — `ok=false` for an empty value (caller skips); UUID value → ` AND <column> = $n` (one arg); alias value → ` AND <column> IN (SELECT cluster_uuid FROM clusters WHERE cluster_alias = $n)` (one arg) — the exact subquery shape of `parseClusterParams` at `utils.go:337`, citation in the doc comment.

- [ ] **Step 1: Write the failing unit test** — table: UUID value → exact predicate; alias value → subquery predicate; empty value → ok=false; arg-index threading (nextIdx correct for both shapes); the SQL fragments contain no org predicate in the subquery (matching the working endpoints' globally-unique-UUID rationale).
- [ ] **Step 2: Run → RED** (`undefined: clusterFilterPredicate`).
- [ ] **Step 3: Implement** per Interfaces; doc comment cites #790, the Koku contract (`provider_map.py:51-54`), and the 200+empty semantics.
- [ ] **Step 4: Run → GREEN.**

### Task 2: Regression tests — RED on the reviewed baseline

**Files:**
- Create: `internal/api/cluster_alias_filter_regression_test.go` (package `api_test`)

**Interfaces:**
- Consumes: `permsApp`, `makeIdentityHeader`, `testutil` constants; Task 1's helper transitively.
- Produces: `seedAliasFilterOrg(t, ctx)` — two clusters in one org, **each with a distinct alias** (`alpha-a` / `beta-b`), each carrying one data row for all five surfaces (node_recommendations, pvc_recommendation_sets, quota_recommendation_sets, cluster_quota_recommendation_sets, snapshot_recommendation_sets).

- [ ] **Step 1: Write the failing tests**, one subtest per endpoint, each covering four cases:
  - **Alias form** → 200, only the aliased cluster's rows (pre-fix: 503 — the RED).
  - **UUID form** → 200, same rows (positive control — already works).
  - **Unknown alias** (`no-such-cluster`) → 200, count 0 (never 503, never 4xx).
  - **Composition**: A-only RBAC grant + alias-of-B → 200, count 0 (the server scope ANDs; the client filter cannot bypass it).
- [ ] **Step 2: Run → RED on baseline** — every alias-form case fails with the 503 (assert the status code first so the failure names the outage, not a count mismatch); controls pass.
- [ ] **Step 3: Ledger the RED output** (which subtests, which status codes).

### Task 3: Apply the helper at the five sites — GREEN

**Files:**
- Modify: `internal/api/handlers_node_utilization.go:246`, `handlers_pvc_filters.go:56`, `handlers_quota_recs.go:164`, `handlers_cluster_quota_recs.go:156`, `handlers_snapshot.go:135`

- [ ] **Step 1:** At each site, replace the raw `AND cluster_uuid = $n` bind with `clusterFilterPredicate(...)` — threading that endpoint's arg indexing; empty-filter guards stay as-is; the #752 scope clauses are untouched (they already sit in the same chains and AND-compose).
- [ ] **Step 2: Run → GREEN** — all Task 2 subtests plus the existing suites for the five endpoints (`TestReaderScope_*`, node-utilization, PVC, quota, CRQ, snapshot tests).
- [ ] **Step 3: `make lint`** (the #753 lesson — always locally) + full `internal/api` package.
- [ ] **Step 4: Commit** (after owner go-ahead): `fix(#790): cluster filter by alias — shared predicate, 200+empty for unknown aliases (Koku contract)`

### Task 4: Mutation check + full verification

- [ ] **Step 1: Mutation** — revert ONE site (PVC) to the raw bind → its alias subtest fails with 503; restore; no remnants.
- [ ] **Step 2: Full suite** — `make test` → PASS (headline claim; no success reported without this output).
- [ ] **Step 3: Commit any strengthening** (after owner go-ahead).

### Task 5: Lab verification (gated on cluster access)

- [ ] **Step 1:** Build a uniquely-tagged image, deploy to the SNO lab per `docs/agents/sno-cluster-operations.md`, and verify the acceptance's UI item: Node and Storage tabs render with a cluster filter chip applied (alias form). Record the tag and observations in the close-out. If the lab is unreachable, record the step as **asserted, not verified** with the named next step.

### Task 6: Close-out

- [ ] **Step 1: Comment on #790** — the owner decisions with their citations (200+empty per Koku `provider_map.py`; display symptom dismissed as integration config with the COALESCE evidence; symptom-1 staleness corrected), the mutation result, the adversarial answer ("how could these tests pass while the bug lives?" — the alias-form subtests fail with 503 pre-fix, naming the outage; the composition subtest pins filter/scope ANDing).
- [ ] **Step 2: Close with the completion-report table** — verified vs asserted (CI on the pushed commits; lab UI verification per Task 5's outcome). Note the IQE cross-repo follow-up: alias-form cluster-filter coverage in iqe-ros-ocp-plugin.

---

## Self-review notes

- **Spec coverage:** acceptance item 1 (both forms 200 on all five endpoints, count included) → Tasks 2-3; item 2 (shared resolution, single/ANY agreement — the ANY half is the #752 clause, preserved) → Tasks 1, 3; item 3 (regression failing pre-fix, mutation-checked; unknown-alias contract decided and documented — owner-decided 200+empty) → Tasks 2, 4, 6; item 4 (display) → dismissed per owner ruling, recorded in Task 6; item 5 (lab UI) → Task 5.
- **Proportion:** one helper + five one-line-shaped call-site changes + one regression file; no error-semantics change, no response-shape change, no UI change.
