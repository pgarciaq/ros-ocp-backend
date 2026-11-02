# #754 Fleet Heatmap Node-Scope Cache Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fleet heatmap responses never disclose nodes outside the caller's `openshift.node` grants — on cold *and* warm paths — while keeping the org-scoped cache and its utility.

**Architecture:** Move node filtering from the cold path (pre-cache, currently bypassed on hits) to a per-request post-cache stage. The cache stores an org-level pre-truncation node pool; every request applies [node filter → maxNodes truncate → Count/warnings] into a fresh response object. Cached objects are never mutated.

**Tech Stack:** Go, echo, pgx/v5, testcontainers, testify; `internal/fleetheatmap` LRU cache (unchanged mechanics).

**Spec:** [GH #754](https://github.com/pgarciaq/ros-ocp-backend/issues/754) (V15-04, P1) + session recon 2026-11-02 (all facts below verified at HEAD `9f226e54`; zero drift from reviewed baseline `3061a76f`).

**Recon-verified facts this plan argues from:**

- `fleetsummary.CacheKey` returns `org:all` for wildcard-cluster users (the leak: shared across node scopes) and `org:rbac:<hash-of-entire-perms-map>` for cluster-scoped users (accidentally node-safe — hash includes `openshift.node`). Heatmap key adds `:heatmap:metric:term:engine[:cluster=...]`.
- `GetFleetHeatmap` (`internal/api/handlers_fleet_heatmap.go`): cache lookup at :139-142 returns **before** the node filter at :239-251; the filter runs cold-path only; the fully-built response (Count baked in) is cached at :304. Both directions leak: unrestricted-first serves secret nodes to the restricted user; restricted-first poisons the unrestricted user with a partial entry.
- `openshiftNodeRBACScope` (`handlers_node_recs.go:878`): restricts only when RBAC enabled AND no global wildcard AND `openshift.node` present AND not `*`.
- Response shape: `Data []FleetHeatmapNode` + `Meta{Count, Metric, Term, Engine, LatestUpdate, DataWindow, Currency, Warnings}` — no cross-node aggregates; post-cache filtering is filter-Data + recompute-Count/warnings.
- Currency is org-level (`resolveUserCurrency(ctx, orgID)`) — not a per-user leak; conversion can stay at pool-build time.
- maxNodes: SQL caps at `maxNodes+1`; cold order is cap(SQL) → node-filter → truncate → warn. The cache must hold the **pre-truncation** pool (≤ maxNodes+1) so per-request [filter → truncate → Count → cap-warning] reproduces cold semantics exactly for restricted users.
- Current filter mutates in place (`nodes[:0]`, :244) — the fix must build new slices.
- The heatmap is the only cached node-scoped surface (gpu-timeslicing detail, node detail, machinesets, node-recs persist: no cache usage — grep-verified). Fleet/savings summary node-scope is a separate question: #789.
- Test pattern (verified in `settings_rbac_test.go`): `t.Setenv("RBAC_ENABLE", "true")` + `config.ResetForTest()`; echo middleware injecting `Identity` orgID + `c.Set("user.permissions", perms)`; routes via `RegisterV1RoutesForTest`. Heatmap fixtures: `seedHeatmapData` + `fleetheatmap.ResetForTest()` / `InvalidateOrg(orgID)` in `handlers_fleet_heatmap_integration_test.go`.

## Global Constraints

- **Production change** (unlike #779): touches `internal/api/handlers_fleet_heatmap.go` and possibly `internal/fleetheatmap` — commits wait for explicit owner go-ahead (repo approval gate).
- **Pre-commit adversarial review** (repo rule for auth/RBAC patches): the closing comment must answer "how could these tests pass while the bug lives?"
- Cache key dimensions preserved: org, cluster-scope discriminator (`rbacScoped`), client cluster filter, term, engine, metric. No new key dimensions; no TTL change.
- Solve the demonstrated case; no drive-by refactors, no cache-mechanism rewrite.
- No `t.Skip`, no weakened assertions; every new assertion carries its rules-out sentence (adversarial-fixtures.md).
- Push target: fork (`pgarciaq`), branch `pgarciaq-rosocp-superpowers-phase17`.
- Out of scope: #789 (summary aggregates), #545/#148 history.

## Review Focus

1. **Reverse-warm poisoning** — a restricted user warming first must not shrink the unrestricted user's view. → Task 1 Test B.
2. **Shared-cache mutation** — per-request filtering must not mutate the cached pool (no `[:0]` aliasing); a restricted read must not corrupt a later unrestricted read. → Task 1 Test C.
3. **maxNodes per-user semantics** — per-user truncation, cap warning, and Count must match cold-path behavior (pool pre-truncated, filter-then-truncate per user). → Task 1 Test D (small configured maxNodes).
4. **Cluster dimension unregressed** — cluster-scoped users keep distinct `org:rbac:<hash>` entries and never hit `org:all`. → Task 1 Test E.
5. **RBAC-disabled path unchanged** — `RBAC_ENABLE=false` serves all nodes, cold and warm. → Task 1 Test F.

---

### Task 1: Regression tests — RED on the reviewed baseline

**Files:**
- Create: `internal/api/handlers_fleet_heatmap_rbac_test.go` (package `api_test`)
- Consumes: `seedHeatmapData`-style seeding (new seed variant: two nodes in one org/cluster — `visible-node`, `secret-node` — in `node_recommendations`), `fleetheatmap.ResetForTest()`/`InvalidateOrg`, the `settings_rbac_test.go` middleware pattern.

**Interfaces:**
- Produces: test app builder `newHeatmapRBACApp(t, orgID, perms)` returning an echo app with identity + permissions middleware and heatmap routes; seed helper `seedHeatmapNodes(t, ctx, orgID, clusterUUID, nodeNames ...string)`.

- [ ] **Step 1: Write the failing tests** (all in one file; shared builder/seed helpers)

```go
// Test A (#754 criterion 1 — the demonstrated leak): unrestricted user warms
// org:all; a wildcard-cluster/narrow-node user then hits the same key and
// must NOT receive secret-node. Rules out: cache hit bypassing node scope.
// Expected on baseline: FAIL — count=2 including secret-node.

// Test B (#754 criterion 2 — poisoning): restricted user warms first;
// the unrestricted user must still see BOTH nodes on the shared key.
// Rules out: a restricted first caller shrinking the shared entry.
// Expected on baseline: FAIL — count=1.

// Test C (criterion 4 — immutability/parity): same data; restricted user
// cold response and warm response are equal (deep equality), and a
// subsequent unrestricted request still sees count=2 after the restricted
// warm read. Rules out: per-request filtering mutating the cached pool.

// Test D (Review Focus 3): config FleetHeatmapMaxNodes=2, three visible
// nodes granted; restricted user sees exactly 2 + cap warning; unrestricted
// sees 2 + cap warning; Count matches Data length in both.

// Test E (Review Focus 4): cluster-scoped perms (openshift.cluster=[uuid])
// and wildcard-cluster perms produce distinct cache behavior — the
// cluster-scoped user's response is unaffected by an org:all warm entry
// and vice versa.

// Test F (Review Focus 5): RBAC_ENABLE=false → restricted-looking perms
// still see all nodes, cold and warm.
```

- [ ] **Step 2: Run — verify RED for the right reasons**

Run: `go test ./internal/api/ -run 'TestFleetHeatmap_RBAC' -count=1 -v`
Expected: Tests A and B FAIL naming node-scope leakage (A: secret-node present on hit; B: partial view served to unrestricted). Tests C-F pass or fail per current behavior — record actual results; C/D/E/F assertions encode the post-fix contract.

- [ ] **Step 3: Ledger the RED output** (which tests failed, with which messages)

### Task 2: The fix — org-level cache payload + per-request node stage

**Files:**
- Modify: `internal/api/handlers_fleet_heatmap.go`
- Test: Task 1's file turns green.

**Interfaces:**
- Produces: `fleetHeatmapCachePayload` struct — `Nodes []FleetHeatmapNode` (≤ maxNodes+1, pre-truncation, org-level), `LatestUpdate string`, `ScanErrors int`, `DisplayCurrency string`, `Warnings []string` (org-level scan warning only). Stored via existing `fleetheatmap.Put` (payload, not response).
- Produces: `applyNodeScopeToHeatmap(p fleetHeatmapCachePayload, userPerms map[string][]string, maxNodes int) FleetHeatmapResponse` — pure function: node filter into a **new** slice (never `[:0]`), truncate to maxNodes, build Meta (Count = len(Data), cap warning iff truncated, scan warning from payload, currency/metric/term/engine from payload+params).

- [ ] **Step 1: Implement the payload + per-request stage; restructure the handler**

Cold path: SQL (cluster filter as today) → build payload (no node filter, no truncation, no Count) → `Put(cacheKey, payload)` → `applyNodeScopeToHeatmap` → respond. Warm path: `Get` → type-assert payload → `applyNodeScopeToHeatmap` → respond. Empty-allowedClusters early return (current :160-166) unchanged, before caching. The in-place `nodes[:0]` filter at :244 is removed (subsumed).

- [ ] **Step 2: Run — verify GREEN**

Run: `go test ./internal/api/ -run 'TestFleetHeatmap' -count=1 -v`
Expected: PASS — all Task 1 tests plus the existing heatmap tests (`CacheHit`, `MetricParam`, `EmptyState`, integration) stay green.

- [ ] **Step 3: Commit** (after owner go-ahead)

```bash
git add internal/api/handlers_fleet_heatmap.go internal/api/handlers_fleet_heatmap_rbac_test.go
git commit -m "fix(#754): fleet heatmap applies node RBAC scope on every request, cache holds org-level pool"
```

### Task 3: Mutation check + adversarial pass + verification

- [ ] **Step 1: Mutation check** — remove the `applyNodeScopeToHeatmap` filter call (return unfiltered). Run Task 1 tests. Expected: A and B FAIL naming node-scope leakage. Restore. (If they pass, the tests are blind — stop and strengthen.)
- [ ] **Step 2: Weakened-fixture pass** (adversarial-fixtures §3) — same node names across orgs (collision), reversed seed order, single-node grants; confirm each test still isolates the mechanism.
- [ ] **Step 3: Affected suites** — `go test ./internal/api/... ./internal/fleetheatmap/... -count=1` → PASS.
- [ ] **Step 4: Full suite** — `make test` → PASS (headline claim; no success reported without this output).
- [ ] **Step 5: Commit any test-strengthening** (after owner go-ahead)

### Task 4: Close-out

- [ ] **Step 1: Comment on #754** — findings, mutation-check results, the pre-commit adversarial answer ("how could these tests pass while the bug lives?": they cannot — both warm orders + cold/warm parity + mutation check pin the mechanism), #789 cross-reference.
- [ ] **Step 2: Close with the completion-report table** — verified vs asserted (CI workflows on the pushed commits; the RBAC-disabled live-cluster path is covered by Test F locally, not by a live run).

---

## Self-review notes

- **Spec coverage:** criterion 1 → Test A; criterion 2 → Test B; criterion 3 (dimensions preserved) → key construction unchanged + Test E; criterion 4 (no shared mutation, cold/warm parity) → Test C; criterion 5 (regression fails on baseline, right reason, mutation check) → Task 1 RED + Task 3 Step 1; criterion 6 (suites, executed vs skipped) → Task 3; criterion 7 (ancillaries) → Task 4 table; criterion 8 (completion report) → Task 4.
- **Proportion:** one handler + one new test file; the response contract, cache mechanics, and key dimensions are all preserved — no UI/IQE/OpenAPI surface changes (response shape identical; filtering only removes rows the caller was never entitled to).
