# #752 Resource-Reader Cluster Scope — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Readers that today serve org-wide data to cluster-scoped roles — PVC, quota, cluster-quota (lists, groups, CSV), HCP advisories, and the resolved-ID detail/trend paths — enforce the caller's server-owned cluster scope, per ADR-0352's helper contract.

**Architecture:** Promote #753's `snapshotDataClusterScope` to a shared `dataClusterScope` helper; every list/count/group/CSV query gains `AND <cluster-col>::text = ANY($N)` with an empty-set early return; every resolved-ID path (detail/trend/OOM timeline) resolves the row's cluster and returns **404** when it is outside the caller's scope — Koku's convention, verified: `CostModelViewSet.get_queryset()` filters the queryset by RBAC and `retrieve` uses DRF's `get_object()`, so an out-of-scope UUID is indistinguishable from a nonexistent one.

**Tech Stack:** Go, echo, pgx, testcontainers, testify; the #753/#754 test patterns (`permsApp`, hit-proofing, config-leak hygiene).

**Spec:** [GH #752](https://github.com/pgarciaq/ros-ocp-backend/issues/752) (V15-02, P1) + its handoff comment (proposed direction: server-owned scope on list/count/group/CSV and resolved-ID detail/history; do not substitute a client filter for authorization; test two distinct clusters in one org, wildcards, empty grants, foreign-org negatives) + owner decisions 2026-11-03 (404-for-out-of-scope detail, matching Koku; all source-read sites).

**Recon-verified facts this plan argues from:**

- Zero drift since reviewed baseline `3061a76f` on all seven named files.
- List readers build `WHERE org_id = $1 + filterSQL` where the only `cluster_uuid` predicate is the **client-supplied** filter param (`handlers_pvc.go:161,173`; `handlers_pvc_group.go:38,48`; `handlers_quota_recs.go:212,237,361,379`; `handlers_cluster_quota_recs.go:205+` and its group sites at `:343-360`). A client filter is not authorization.
- HCP advisories (`handlers_hcp_advisories.go:150+`): raw `WHERE org_id = $1 AND expires_at > now()` over `hcp_correlation_advisories`; the scope column is **`management_cluster_uuid`** (verify its UUID space against `clusters` at implementation).
- OOM timeline (`handlers_oom_timeline.go:19+`, `model/oom_timeline.go:38-78`): `ResolveContainerKeyByID` (org-scoped, correct) resolves the cluster; `QueryOOMTimeline` then runs unscoped. The existing unknown-ID path returns 404 — the out-of-scope guard extends it.
- Source-read detail/trend sites (issue-flagged): `handlers_pvc_detail.go:61`, `handlers_quota_detail.go:109` (quota) and `:265` (CRQ), `handlers_quota_trend.go:19` (`/quota/:quota-id/trend`).
- #753 assets to reuse: `snapshotDataClusterScope` (promote to shared `dataClusterScope`), `filterClustersByRBAC` (fixed; eligibility semantics inherited transitively — settings-only users get an empty scope), and the empty-slice-means-no-filter hazard documentation (ADR-0352 consequences).
- The grouped/count/CSV query builders on these readers share the `filterSQL` concatenation shape — the scope clause threads through the same mechanism.

## Global Constraints

- Production change across ~10 files — commits wait for explicit owner go-ahead (repo approval gate).
- **Pre-commit adversarial review** (auth/RBAC class): the closing comment answers "how could these tests pass while the bug lives?"
- **404 for out-of-scope resolved IDs** (owner decision, Koku-verified) — indistinguishable from unknown IDs; lists keep 200+empty. No other error-semantics changes; response shapes unchanged.
- Server scope COMPOSES with the client cluster filter (intersection): a user filtering for a cluster outside their scope gets empty, never the client filter replacing authorization.
- Empty-set early returns before every query (the engine/`len(x)>0` no-filter hazard); grouped and CSV shapes preserved on the empty path.
- No drive-by refactors beyond the helper promotion (it IS the mechanism); #753's three snapshot/VM call sites update with it.
- No `t.Skip`, no weakened assertions; rules-out comments per adversarial-fixtures.md; `t.Cleanup(config.ResetForTest)` at every RBAC-affecting Setenv site (ADR-0352 test-hygiene consequence).
- Push target: fork (`pgarciaq`), branch `pgarciaq-rosocp-superpowers-phase17`.

## Review Focus

1. **404 indistinguishability** — out-of-scope and unknown IDs return byte-identical 404s on every detail/trend path. → Task 2 tests.
2. **Empty-set early return** — no list/count/group/CSV query runs with an empty scope slice (the no-filter hazard). → Task 3 + mutation.
3. **Client filter composes, never replaces** — A-only user filtering for B gets empty; wildcard filtering for B gets B. → Task 2 tests.
4. **Shape preservation** — grouped and CSV empty paths return their requested shapes. → Task 2 tests.
5. **Advisories column** — `management_cluster_uuid` is the scope key; verify the UUID space and that the wildcard path is unchanged. → Task 3.
6. **The full handoff matrix** — two distinct clusters in one org, wildcards, empty grants, foreign-org negatives (same UUID in another org). → Task 2.

---

### Task 1: Promote the shared scope helper

**Files:**
- Create: `internal/api/handlers_rbac_scope.go` — `dataClusterScope(ctx, orgID, userPerms) (clusters []string, scoped bool, err error)` (moved from `handlers_snapshot_cost_by_type.go`, doc comment updated to cite ADR-0352 and #752).
- Modify: the three #753 call sites (`handlers_snapshot_cost_by_type.go`, `handlers_snapshot_age_distribution.go`, `handlers_vm_instance_types.go`) and remove the old declaration.

- [ ] **Step 1:** Move + rename; update call sites; `go build` + `go test ./internal/api/ -run 'TestRBAC_Eligibility|TestGetSnapshot|TestGetClusterInstanceTypes' -count=1` green (no behavior change — pure relocation).
- [ ] **Step 2: Commit** (after owner go-ahead): `refactor(#752): promote dataClusterScope as the shared reader scope helper (from #753)`

### Task 2: Regression tests — RED on the reviewed baseline

**Files:**
- Create: `internal/api/resource_reader_scope_regression_test.go` (package `api_test`)

**Interfaces:**
- Consumes: `permsApp`, `makeIdentityHeader`, `testutil` constants; `dataClusterScope` transitively.
- Produces: `seedTwoClusterOrg(t, ctx)` — clusters A/B in one org with the repro's data: B PVC (capacity `987654321`, savings `123.45 USD`), B quota resources, an unexpired B management advisory, a B container with **7 OOM events** (`librobne` OOM fixtures or direct `oom_*` seeding per the model's table); A equivalents.

- [ ] **Step 1: Write the failing tests** (the issue's repro, per surface):
  - PVC list + group + CSV: A-only sees A's rows only (baseline: B's capacity/savings served — the exact repro); wildcard sees both; empty grant denies; client-filter-for-B as A-only → empty.
  - Quota list + group; CRQ list + group: same matrix.
  - Advisories: A-only does not see B's management advisory; wildcard does.
  - OOM timeline: A-only requesting B's resolved container-id → **404 identical to an unknown id**; wildcard → 200 with 7 entries.
  - Detail/trend (PVC detail, quota detail, CRQ detail, quota trend): A-only → 404 identical to unknown id; wildcard → 200.
  - Foreign-org negative: same UUID in another org never discloses.
- [ ] **Step 2: Run → RED on baseline** for every A-only assertion, with mechanism-naming messages; positive controls (wildcard) and the unknown-ID 404 parity assertions record baseline behavior.
- [ ] **Step 3: Ledger the RED output.**

### Task 3: Wire the list readers

**Files:**
- Modify: `internal/api/handlers_pvc.go`, `handlers_pvc_group.go`, `handlers_quota_recs.go`, `handlers_cluster_quota_recs.go`, `handlers_hcp_advisories.go`

- [ ] **Step 1:** In each handler after param parsing: `allowed, scoped, err := dataClusterScope(ctx, orgID, get_user_permissions(c))` → 503 on error; `scoped && len(allowed) == 0` → the surface's empty shape (list meta-zero, group empty-data, CSV header-only — mirroring #753's shape-aware empty paths). Otherwise append ` AND <cluster-col>::text = ANY($N)` to filterSQL (threading the args index; the advisories column is `management_cluster_uuid`).
- [ ] **Step 2: Run → GREEN** for all list/group/CSV assertions (detail/trend may remain red — Task 4). Commit (after owner go-ahead): `fix(#752): list, group and CSV readers enforce server-owned cluster scope`

### Task 4: Wire the detail/trend paths — 404 for out-of-scope

**Files:**
- Modify: `internal/api/handlers_oom_timeline.go`, `handlers_pvc_detail.go`, `handlers_quota_detail.go`, `handlers_quota_trend.go`

- [ ] **Step 1:** After each path resolves its row/cluster: `if fleetSummaryNeedsClusterFilter(userPerms) && len(filterClustersByRBAC([]string{resolvedCluster}, userPerms)) == 0 { return <the handler's existing not-found response> }` — byte-identical to the unknown-ID 404.
- [ ] **Step 2: Run → GREEN** for all detail/trend assertions. Commit (after owner go-ahead): `fix(#752): resolved-ID detail and trend paths return 404 outside the caller's cluster scope (Koku convention)`

### Task 5: Mutation checks + adversarial pass + full verification

- [ ] **Step 1: Mutations** — remove the scope clause from ONE list reader (its test fails); remove ONE detail guard (its 404 test fails). Restore; no remnants.
- [ ] **Step 2: Adversarial controls** — reversed seed order; same-value fixtures across the two clusters (UUID-asserted, not name-asserted); the client-filter composition re-verified post-fix.
- [ ] **Step 3: `make lint`** (the #753 lesson — never skip golangci-lint locally) + full `make test` → PASS.
- [ ] **Step 4: Commit any strengthening** (after owner go-ahead).

### Task 6: Close-out

- [ ] **Step 1: Comment on #752** — findings, the ten-site inventory, mutation results, the adversarial answer, the Koku-verified 404 decision with its source citation, cross-references (ADR-0352, #753's helper foundation, iqe-ros-ocp-plugin same-org multi-cluster negative coverage as the named follow-up).
- [ ] **Step 2: Close with the completion-report table** — verified vs asserted (CI on the pushed commits; live-cluster validation not performed).

---

## Self-review notes

- **Spec coverage:** the handoff's proposed direction (list/count/group/CSV + resolved-ID detail/history; no client-filter substitution; the four-matrix tests) maps to Tasks 2-4; the issue's source-read note (detail/trend) is in scope per the owner decision; close criteria follow the v15 template (RED on baseline, right reason, mutation checks, suites, ancillaries, completion report).
- **Proportion:** one helper promotion + five list wirings + four detail guards + one regression file; no response-shape changes beyond the owner-approved 404 on paths that today leak data.
