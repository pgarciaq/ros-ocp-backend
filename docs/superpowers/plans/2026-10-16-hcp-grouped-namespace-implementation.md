# #690: Grouped-by-Namespace Management View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the Management overhead sub-tab its own grouped view (per-namespace counts + savings, drillable to the filtered flat list) — the real content split #689 v1 deferred.

**Architecture:** Mirror the hosted grouped path exactly: new `group_by[namespace]` branch on the HCP endpoint (model aggregation over pinned rows, no new tables), UI sub drives it, grouped table branch extended with namespace columns. Backend and UI land together; single backend rollout also carries #692's alias read fix (`58cf7b6f`).

**Tech Stack:** Go (gorm, pgx testcontainers), React + TypeScript (PatternFly, Jest, formatjs translations pipeline).

**Spec:** #690 body + gates (v1 shipped ✓); #689 narrowing (Management = HCP-namespaced + unassociated; grouped-namespace is the split mechanism).

## Global Constraints

- No schema changes (no migration): aggregation only, mirroring `GetHCPGroupedRecommendations` (`recommendation_hcp.go:179`).
- Unassociated rows MUST appear (grouped-hosted explicitly excludes them; namespace grouping includes every in-scope row — namespace is never empty).
- Term/engine projection identical in the new view (toolbar untouched; grouped queries pin short/cost server-side via `hcpPinned`).
- i18n: reuse existing messages where possible (`Recommendations`, savings); new strings go through the translations pipeline.
- Landing: backend → fork `pgarciaq/ros-ocp-backend` phase17; UI → fork `pgarciaq/koku-ui` phase17. Native hypervisor builds; unique tags; Quay + internal mirror; single backend rollout shared with #692 read fix.
- TDD: red-first regression tests naming the mechanism, mutation-checked per repo procedure.

## Review Focus

- Namespace groups with zero pinned rows never render phantom groups (pinned short/cost filter applies before grouping, same as hosted) — pinned by Task 1 tests.
- Drill-down from a namespace group lands on the filtered flat list with term/engine preserved (not a dead group row) — pinned by Task 3 tests.
- Unassociated namespaces (ros-demo shape: empty hosted ID) appear under their own namespace, never dropped, never merged into another group — pinned by Task 1 fixtures (distinct namespaces + one empty-ID namespace asserting presence).
- The `sub` + `group_by` URL pair round-trips: Management sub sets namespace grouping in the same atomic navigation (the #689 race fix pattern — extras ride setQuery) — pinned by Task 3 tests.
- Grouped savings use the same pinned-rows + display-currency path as hosted (no parallel math) — pinned by Task 1 asserting cents match a hand-computed SUM fixture.

---

### Task 1: Backend namespace grouping + tests

**Files:**
- Modify: `internal/model/recommendation_hcp.go` (new `GetHCPNamespaceGroupedRecommendations` + row struct)
- Modify: `internal/api/handlers_hcp.go` (accept `group_by[namespace]`, route to it)
- Test: `internal/api/handlers_hcp_test.go` (mirror `TestHCPList_GroupByCounts` fixtures)

**Interfaces:**
- Consumes: `applyHCPBase` (scoped/RBAC query), `hcpPinned` (short/cost representative rows), `listoptions` limit/offset.
- Produces: `[]HCPNamespaceGroupedRow{ Namespace string, Count int64, SavingsCents int64 (+ EstimatedSavings rendering in handler) }` via `GET /recommendations/openshift/hcp?group_by[namespace]=*`, ordered namespace ASC. Includes unassociated rows (no hosted-ID filter — the deliberate difference from the hosted branch).

- [ ] **Step 1: Write the failing test**

```go
// Seeds: two HCP namespaces (one with an associated + one unassociated row
// sharing workload names adversarially), one app namespace (must not appear).
// Asserts per-namespace count/savings rows exist for both HCP namespaces
// (unassociated included), app namespace absent, savings equal hand SUM.
func TestHCPList_GroupByNamespaceCounts(t *testing.T)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run 'TestHCPList_GroupByNamespaceCounts' -v -count=1`
Expected: FAIL (unknown group_by → falls through to list or 400; no namespace groups)

- [ ] **Step 3: Implement model func + handler branch** (mirror hosted: pinned → distinct-namespace count → select namespace/count/savings → group → ASC → page → scan)

- [ ] **Step 4: Run test to verify it passes**

Run: same as Step 2
Expected: PASS

- [ ] **Step 5: Run affected suites**

Run: `go test ./internal/api/ -run 'TestHCP' -count=1`, `go test ./internal/model/ -count=1`, `gofmt -l` on touched files
Expected: PASS, clean

- [ ] **Step 6: Commit**

```bash
git add internal/model/recommendation_hcp.go internal/api/handlers_hcp.go internal/api/handlers_hcp_test.go
git commit -s -m "feat(api): grouped-by-namespace HCP view (#690)"
```

### Task 2: UI Management grouped view + tests

**Files:**
- Modify: `optimizationsHcpTable.tsx` (Management sub sets `group_by: {namespace: '*'}` via atomic setQuery extras; grouped branch renders namespace/count/savings columns; drill-down filters project)
- Modify: `hcpTableUtils.ts` (`HcpGroupBy` gains `'namespace'`; `hcpSubToGroupBy`: hosted→`''` + strip, overhead→`'namespace'`)
- Test: `hcpTableUtils.test.ts`, `hcpSummaryStrip.test.ts` untouched (strip stays hosted-only)
- Modify: `locales/messages.ts` only if a new label is unavoidable (reuse `namespace`, `Recommendations`, savings strings)

**Interfaces:**
- Consumes: Task 1 endpoint; existing grouped-row rendering pattern (hosted columns as template); existing drill-down (filter key becomes `project`).
- Produces: Management sub opens namespace-grouped; chip/row click → filtered flat list with term/engine preserved; strip remains hosted-only.

- [ ] **Step 1: Write the failing tests**

```tsx
// hcpSubToGroupBy('overhead') === 'namespace'; grouped fixture row renders
// namespace/count/savings cells; drill-down emits project filter (not hosted_cluster_id)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `npx jest hcpTableUtils --no-cache` in `apps/koku-ui-ros`
Expected: FAIL (mapper returns '')

- [ ] **Step 3: Implement wiring + columns** (mirror hosted grouped branch with namespace/count/savings cells; keep strip hosted-only)

- [ ] **Step 4: Run tests to verify they pass**

Run: `npx jest hcpTableUtils hcpSummaryStrip useHcpSubUrl --no-cache`, `npx tsc --noEmit -p tsconfig.json`, `npx eslint` on touched files
Expected: PASS, clean

- [ ] **Step 5: Commit + push fork**

### Task 3: Translations, full verification, images, rollout

**Files:** (generated catalogs only)

- [ ] **Step 1: Translations pipeline** (only if Task 2 added strings; skip with note if reused)

Run in `apps/koku-ui-ros`: `npm run translations`
Expected: exit 0

- [ ] **Step 2: Full UI suite**

Run: `npm test -- --no-cache`
Expected: PASS except the 12 documented pre-existing failures (byte-identical set on clean tree — re-verify, don't assume)

- [ ] **Step 3: Backend full affected suites**

Run: `go test ./internal/api/ ./internal/model/ ./internal/engine/... -count=1` (or documented subset with rationale)
Expected: PASS

- [ ] **Step 4: Native hypervisor builds (NOT emulation), unique tags**

Backend image (with #692 `58cf7b6f` + Task 1) and UI image (with Task 2), from submodule-aware sync (`rsync --exclude node_modules --exclude .git`, never bare `git archive`)

- [ ] **Step 5: Push (Quay + skopeo internal mirror), set images, rollout watches**

- [ ] **Step 6: Live verification on SNO** (Management sub shows namespace groups incl. unassociated; drill-down filters; farm column shows friendly names post-`58cf7b6f` — closing #692's live proof simultaneously)

- [ ] **Step 7: Report on #690** (completion table per properly-implement-feature; chat summary)
