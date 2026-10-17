# Management overhead scope — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the UI's "Management overhead" sub-tab serve *management-cluster namespaces that are not hosted control planes*, instead of duplicating the Hosted clusters view.

**Architecture:** One additive query parameter (`filter[scope]=management`) on the three HCP endpoints swaps the scope fragment behind the existing surface: list, namespace-grouped, and detail gate. Default scope (HCP evidence) is untouched. The `clusters` row already carries what the new scope needs (`cluster_topology`, persisted by the W0 processor path — live-verified: `hcp-mgmt|management`, `hc01|hosted`, `hc02|hosted`, `sno|dedicated`), so **no migration and no operator change**.

**Tech Stack:** Go (echo + gorm backend, raw-SQL fragments with EXISTS subqueries), TypeScript/React UI.

**Spec:** Approved direction (2026-10-17, owner): Management overhead = management-cluster namespaces ∉ `hcp_namespaces`. Experiment evidence on #694: MCE flows through collection + standard container endpoint (HTTP 200, `meta_count=25`) but is dropped by `hcpScopeFragment` (not in `clusters.hcp_namespaces`, absent from `manifest_hcp_snapshots`).

## Global Constraints

- No JOIN against `clusters` — EXISTS subqueries only (#600/#525 one-row-per-UUID invariant).
- RBAC filter applies before any scope filtering (`applyHCPBase` pattern, `recommendation_hcp.go:83-103`).
- No migration: `clusters.cluster_topology` exists (migration ≥ 000196; normalize-through-`String()` semantics in `librobne/pgrec/cluster.go:139-156`).
- Default HCP scope and all existing responses byte-identical when the param is absent.
- Pinned collapse (`term IN ('short','short_term') AND engine='cost'`) reused for counts/savings, per #690 mechanism.
- Regression tests follow `docs/agents/adversarial-fixtures.md` (same-value collisions, SUM-sensitive aggregation; each new test must fail pre-fix for the right reason).

## Review Focus

1. **NULL `hcp_namespaces` on a management cluster** — must read as empty so every collected namespace on it appears in Management overhead (precedent: `hcp_namespaces_test.go:110-117`). Pinned by Task 2 test.
2. **`hosted_cluster_id` set on a management-scope row** — management namespaces have no hosted cluster; if a row somehow carries one, the list must still serve it under management scope (field is display-only there). Pinned by Task 2 test.
3. **Unknown / `dedicated` topology rows** — must never appear under Management overhead (only explicit `management` qualifies). Pinned by Task 2 test.
4. **Drill-down query pollution** — scope param must survive drill-down (`filter[project]`) navigation so the overhead drill-down stays overhead-scoped. Pinned by Task 4 UI test.
5. **Grouped savings arithmetic** — namespace rollup must SUM across same-namespace containers (same-value collisions can't mask a MAX/DISTINCT regression). Pinned by Task 3 test.

---

### Task 1: Model layer — management scope fragment + scope-aware accessors

**Files:**
- Modify: `internal/model/recommendation_hcp.go` (near `hcpScopeFragment`, lines 24-40, and accessor signatures below)
- Test: `internal/api/handlers_hcp_test.go` (fixture + assertions; the API-level test drives the model)

**Interfaces:**
- Consumes: `applyHCPBase(orgID, queryParams, userPerms)`, `hcpScopeFragment()`, `HCPRowScoped(orgID, clusterUUID, namespace)` (existing).
- Produces: `managementScopeFragment() (string, time.Time)`; `applyHCPBase` honoring `filter[scope]=management` by swapping the fragment; `HCPRowScoped` accepting a scope selector (default = HCP scope, unchanged callers).

- [ ] **Step 1: Write the failing tests** — extend the existing seeded fixtures (`hcpTestMC1` management-topped, one `hcpTestNS*`-named hosted NS + one `mgmt-ns` + `mgmt-ns2` with two containers sharing `mgmt-ns2`):

  - `TestHCPList_ManagementScope`: `GET /hcp?filter[scope]=management` → contains `mgmt-ns` rows; does **not** contain the hosted NS; does **not** contain rows seeded on a `hosted`- and a `dedicated`-topology cluster; meta count = distinct containers; savings SUM across the two `mgmt-ns2` containers equals the fixture sum (not MAX).
  - `TestHCPList_ManagementScope_NullHCPNamespaces`: management cluster seeded with `hcp_namespaces = NULL` → all its namespaces appear.
  - `TestHCPDetail_ManagementScope`: detail on a management row with the param → 200; same ID without the param (HCP scope) → 404; hosted row under `scope=management` → 404.
  - `TestHCPList_DefaultScopeUnchanged`: without the param, response matches today's shape (hosted NS present, `mgmt-ns` absent) — guards the byte-identical constraint.
  - Seed helper must write `cluster_topology` — extend `seedCluster` (currently sets `hcp_namespaces` only).

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/api/ -run 'TestHCPList_ManagementScope|TestHCPDetail_ManagementScope|TestHCPList_DefaultScopeUnchanged' -v`; expect FAIL (param unrecognized → management rows absent everywhere).

- [ ] **Step 3: Implement**

  - `managementScopeFragment()`: EXISTS on `clusters` where org/cluster match, `cluster_topology = 'management'`, and `recommendation_sets.namespace != ALL(COALESCE(c.hcp_namespaces, '{}'))`.
  - `applyHCPBase`: read `filter[scope]` from `queryParams`; `management` → fragment swap; any other value → 400-style rejection at handler (Task 2) or ignore-with-default; **decision at implementation** — prefer explicit 400 on unknown values (document choice in implementation comment).
  - `HCPRowScoped` gains a scope param or a sibling `RowScoped(orgID, clusterUUID, namespace, scope)`; existing callers pass HCP scope unchanged.
  - Confirm hypothesis: pinned-collapse and sibling-fan-out paths are fragment-agnostic and need no edits (verify by test passing).

- [ ] **Step 4: Run tests to pass** — same command + `go test ./internal/model/...` short suite green.

- [ ] **Step 5: Mutation check** — stash the fragment swap; confirm Step 1 tests fail naming the mechanism (mgmt rows missing), not an incidental property; restore.

- [ ] **Step 6: Commit** — `feat(api): management-overhead scope on HCP surface (#694)`.

### Task 2: Handlers — param parsing + validation

**Files:**
- Modify: `internal/api/handlers_hcp.go` (list ~line 34, grouped ~line 137, detail ~line 181)
- Test: `internal/api/handlers_hcp_test.go`

**Interfaces:**
- Consumes: Task 1 model accessors.
- Produces: `filter[scope]` accepted on `GET /recommendations/openshift/hcp`, `/hcp/:recommendation-id`; unknown values → 400 `{"error": ...}` consistent with existing filter validation.

- [ ] **Step 1: Failing test** — `filter[scope]=nonsense` → 400; `filter[scope]=management` + `group_by[namespace]` → 200 grouped rows scoped to management (feeds Task 3); absent scope → 200.
- [ ] **Step 2: Verify fail** — expect 200 today (param silently ignored).
- [ ] **Step 3: Implement** — parse/validate in the three handlers before model call; thread the scope value through.
- [ ] **Step 4: Verify pass** — full `go test ./internal/api/ -run TestHCP` green.
- [ ] **Step 5: Commit** — folded into Task 1's commit if same working tree is clean; else `feat(api): validate scope param on HCP handlers (#694)`.

### Task 3: Grouped namespace view under management scope

**Files:**
- Modify: `internal/model/recommendation_hcp.go` (`GetHCPGroupedRecommendations` / namespace-grouped sibling)
- Test: `internal/api/handlers_hcp_test.go`

**Interfaces:**
- Consumes: `HCPNamespaceGroupedRow` (lines 70-78), Task 1 fragment.
- Produces: `group_by[namespace]&filter[scope]=management` returns one row per management namespace with COUNT + SUM(savings_cents), unassociated rows included (#690 mechanism, now with management scope).

- [ ] **Step 1: Failing test** — seed two containers in one mgmt namespace with known savings cents (values with a same-cents collision, e.g. 300000+300000 distinct containers); assert one grouped row, `count=2`, savings = exact sum.
- [ ] **Step 2: Verify fail** (fragment swap not wired into grouped path).
- [ ] **Step 3: Implement** — scope swap in the grouped path (same `applyHCPBase` origin, so likely free from Task 1; confirm, don't assume).
- [ ] **Step 4: Verify pass** — `go test ./internal/api/ -run TestHCP` green; `make test-short` green (full short suite).
- [ ] **Step 5: Mutation check** — swap SUM for MAX in fixture mentally: the test must be built so a MAX implementation would produce a different number (same-value collision guarantees this).
- [ ] **Step 6: Commit** — with Task 1/2 if atomic; else separate.

### Task 4: UI — pass scope on the overhead sub-tab

**Files:**
- Modify: `koku-ui/apps/koku-ui-ros/src/routes/optimizations/optimizationsTable/optimizationsHcpTable/hcpTableUtils.ts` (`hcpSubToGroupBy`, add scope mapping) and the query assembly in `useHcpRecommendationsReport.ts` / `optimizationsHcpTable.tsx` (exact wiring = mechanism hypothesis to confirm against current param threading)
- Test: `hcpTableUtils.test.ts`, `useUrlState.test.tsx` (drill-down preservation)

**Interfaces:**
- Consumes: backend `filter[scope]=management` (Task 2).
- Produces: `sub=overhead` requests carry `filter[scope]=management` alongside `group_by[namespace]`; drill-down (`filter[project]`) preserves it.

- [ ] **Step 1: Failing test** — `hcpSubToGroupBy`/query-builder unit: `sub=overhead` → query contains `filter[scope]=management`; `sub=hosted` → absent; drill-down keeps it.
- [ ] **Step 2: Verify fail**.
- [ ] **Step 3: Implement** — smallest wiring: scope joins the grouped-view param assembly; no new tab, no new route.
- [ ] **Step 4: Verify pass** — `npm test` for the touched suites (koku-ui repo).
- [ ] **Step 5: Commit + push (koku-ui repo, fork)** — explicit scope confirmation per AGENTS.md.

### Task 5: Docs + cross-repo completion table

**Files:**
- Modify: `docs-site/api-reference/` (HCP endpoint: new optional `filter[scope]` values/meaning), `docs/adr/` (ADR-0343: management-overhead scope = topology+not-HCP; amends 0342's boundary statement), both ADR indexes, cheatsheet + Bruno in `costmgmt-api-cheatsheet`.
- Test: `make docs-lint docs-drift docs-sync-check`.

- [ ] **Step 1: ADR-0343** — decision, alternatives (dedicated routes rejected; label-derived scope rejected — backend can't see namespace labels, topology row is the persisted evidence), consequences.
- [ ] **Step 2: docs-site + indexes + Last verified bumps**; run lint/drift/sync-check.
- [ ] **Step 3: Cheatsheet + Bruno** — add scope param to HCP endpoint subsection; rebuild HTML/PDF per its Makefile.
- [ ] **Step 4: IQE** — evaluate REST coverage for the param; add or record tracked follow-up with named evidence.
- [ ] **Step 5: Completion table posted to #694** per `docs/agents/cross-repo-contracts.md`.

## Explicit non-goals

- No change to the default HCP scope, quota queries, or operator collection (w0.5 already live).
- No `open-cluster-management-*`/`klusterlet-*` wave-2 labeling in this plan (separate #694 decision, user-gated).
- No node/PVC/VM surfaces — management scope is container-surface only (demonstrated need).
- No UI redesign of the overhead tab beyond param passing (subtitle refresh already tracked under #694).
