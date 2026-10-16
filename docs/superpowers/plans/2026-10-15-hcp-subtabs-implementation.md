# #689 v1: HCP Sub-Tabs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the HCP Optimizations tab into "Hosted clusters" / "Management overhead" sub-views with a farm column, subtitle, and link-outs — UI-only, no backend change.

**Architecture:** URL-driven sub-tabs mirroring the Storage precedent (`useOptimizationsSubUrl` + PatternFly ToggleGroup): sub selects the existing grouped vs ungrouped HCP views. The farm column, cluster filter, UUID sort, and projection toolbar already exist; the plan wires and labels them.

**Tech Stack:** React + TypeScript, PatternFly ToggleGroup, react-router URL state, Jest, formatjs (translations pipeline).

**Spec:** #689 and its narrowing comments (content, naming, farm column, grouped-deferral, alias hygiene, operator-coverage boundary). v1 bundle checkboxes live on #689.

## Global Constraints

- No backend change: grouped-by-namespace (#690), farm grouping (#691), alias hygiene (#692) are separate gated issues.
- Term/engine projection behavior must be identical in both sub-tabs (OptimizationsProjectionToolbar stays mounted in the shared toolbar).
- New i18n strings go through `messages.ts` + the translations pipeline (`translations:extract`, `translations:compile`, `compile:ast`, `datafile`); the SNO build fails on select/plural branches missing from compiled catalogs.
- Landing: `pgarciaq/koku-ui` branch `pgarciaq-rosocp-superpowers-phase17`. koku-ui tests: `jest --no-cache` in `apps/koku-ui-ros`.
- Sub-tab switching preserves all other URL state (filters, term, engine, pagination base) — mirror `setSub` (replace navigation, keep `location.search` + `location.state`).

## Review Focus

- Switching sub-tabs with active cluster/project/tag filters keeps them applied on both sides, and the back button returns to the prior sub (URL is the state; no shadow component state diverging from it) — pinned by Task 1 tests.
- Grouped↔list transition keeps the selected term+engine (projection travels through `withRosListProjection` on both query paths) — pinned by Task 2 tests.
- Empty states render per sub-tab (a farm with no overhead rows shows the empty state, not the other sub's stale rows) — pinned by Task 2 tests.
- New message catalog entries compile with all select/plural branches (the CI check that caught #652) — pinned by Task 4 running the full translations pipeline, not just extract.
- `order_by[cluster]` sorts UUID text, so same-farm rows group even though aliases display as UUIDs until #692 — pinned by Task 3 asserting the farm column definition carries `orderBy: 'cluster'`.

---

### Task 1: `useHcpSubUrl` hook + tests

**Files:**
- Create: `apps/koku-ui-ros/src/routes/optimizations/useHcpSubUrl.ts`
- Test: `apps/koku-ui-ros/src/routes/optimizations/useHcpSubUrl.test.ts` (new; check if `*.test.ts*` colocated convention — mirror `storageNavigation.test.ts` placement if hooks live beside it, else colocate with the hook)

**Interfaces:**
- Consumes: `react-router-dom` `useLocation`/`useNavigate` (same as `useOptimizationsSubUrl`).
- Produces: `useHcpSubUrl(defaultSub?: HcpSubKey)` returning `{ sub: HcpSubKey; setSub: (next: HcpSubKey) => void }` where `type HcpSubKey = 'hosted' | 'overhead'`. `setSub` sets `sub` param via replace navigation preserving pathname/search/state (does NOT touch `tab`, unlike the storage hook which sets `tab=storage` — the HCP table mounts under tab=hcp already).

- [ ] **Step 1: Write the failing test**

```tsx
// default is 'hosted'; setSub('overhead') navigates to same pathname with sub=overhead, preserving existing params
expect(hook.sub).toBe('hosted');
act(() => hook.setSub('overhead'));
expect(mockNavigate).toHaveBeenCalledWith(expect.stringContaining('sub=overhead'), expect.objectContaining({ replace: true }));
// unknown sub values fall back to default, not crash
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx jest useHcpSubUrl --no-cache` in `apps/koku-ui-ros`
Expected: FAIL (module does not exist)

- [ ] **Step 3: Implement `useHcpSubUrl` in the new file, mirroring `useOptimizationsSubUrl` minus the `tab` set**

- [ ] **Step 4: Run test to verify it passes**

Run: `npx jest useHcpSubUrl --no-cache`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/koku-ui-ros/src/routes/optimizations/useHcpSubUrl.ts*
git commit -m "feat(ros-ui): HCP sub-tab URL state hook (#689)"
```

### Task 2 (revised 2026-10-16 per live feedback): Sub-toggle + flat Hosted view + summary strip

**Files:**
- Modify: `apps/koku-ui-ros/src/routes/optimizations/optimizationsTable/optimizationsHcpTable/optimizationsHcpTable.tsx` (toggle stays; Hosted sub = ungrouped flat list + strip; drill-down chips reuse `handleDrillDownFromGroup`)
- Create: `apps/koku-ui-ros/src/routes/optimizations/optimizationsTable/optimizationsHcpTable/hcpSummaryStrip.tsx` (presentational: per-HC chips from grouped report data)
- Test: `apps/koku-ui-ros/src/routes/optimizations/optimizationsTable/optimizationsHcpTable/hcpSummaryStrip.test.tsx`
- Modify: `hcpTableUtils.ts` (`hcpSubToGroupBy` returns `''` for both subs; grouped machinery stays for old bookmarks and #690)

**Interfaces:**
- Consumes: second `useHcpRecommendationsReport({cluster, query: {...query, group_by: {hosted_cluster_id: '*'}}, skipFetch: sub !== 'hosted'})` call (store is query-keyed — safe); `formatSavingsCellValue` from `../savingsCellUtils`; existing `storageRecommendationCount` message (no new strings).
- Produces: chips `{hosted_cluster_id, count, estimated_savings}`; chip click → existing drill-down (filter chip + overhead sub). Unassociated rows appear in both flat lists with empty hosted cell (no server-side associated-only filter exists — documented, not worked around).

**Why not alternatives:** ungrouped-everywhere without a strip collapses the sub-tabs into one view; inline-expandable groups cost nested-table state for value a strip delivers; grouped-default keeps the extra click the feedback rejects.

**Files:**
- Modify: `apps/koku-ui-ros/src/routes/optimizations/optimizationsTable/optimizationsHcpTable/optimizationsHcpTable.tsx` (mount ToggleGroup + subtitle above toolbar; drive `group_by` from `sub`)
- Modify: `apps/koku-ui-ros/src/locales/messages.ts` (sub labels `hcpSubHosted`, `hcpSubOverhead`; subtitle `hcpTabSubtitle`)
- Test: extend colocated HCP table/toolbar tests (mirror existing `hcpTableUtils.test.ts` patterns)

**Interfaces:**
- Consumes: `useHcpSubUrl` from Task 1; existing `handleOnGroupBySelect(groupBy: HcpGroupBy)` in `optimizationsHcpTable.tsx:209`; existing `OptimizationsProjectionToolbar` (stays mounted, untouched).
- Produces: sub='hosted' sets `group_by` to `'hosted_cluster_id'`; sub='overhead' clears `group_by` (ungrouped list). Behavior change to state explicitly: the tab landing view becomes grouped (was ungrouped list). Filters, term, engine, pagination pass through unchanged.

- [ ] **Step 1: Write the failing test**

```tsx
// rendering with ?sub=overhead issues the HCP query WITHOUT group_by and shows the namespace/management-cluster columns;
// ?sub=hosted (and default, no sub param) issues group_by hosted_cluster_id and shows hosted/count/savings;
// the ToggleGroup reflects the active sub and preserves term+engine across switches
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx jest optimizationsHcpTable --no-cache`
Expected: FAIL (no sub toggle; group_by unchanged by URL)

- [ ] **Step 3: Implement toggle + wiring** (PatternFly ToggleGroup above the toolbar, same Flex layout as `optimizationsStorageDetails.tsx:28-45`; subtitle line under it; link-out to the management-cluster container view)

- [ ] **Step 4: Run tests to verify they pass**

Run: `npx jest optimizationsHcp --no-cache`
Expected: PASS (table, toolbar, utils suites)

- [ ] **Step 5: Commit**

```bash
git add apps/koku-ui-ros/src/routes/optimizations/optimizationsTable/optimizationsHcpTable/ apps/koku-ui-ros/src/locales/messages.ts
git commit -m "feat(ros-ui): HCP Hosted/Management-overhead sub-tabs (#689)"
```

### Task 3: Farm column verification + subtitle/link-outs

**Files:**
- Modify (only if gaps found): `apps/koku-ui-ros/src/routes/optimizations/optimizationsTable/optimizationsHcpTable/optimizationsHcpDataTable.tsx`
- Test: `hcpTableUtils.test.ts` (add coverage if missing)

**Interfaces:**
- Consumes: row fields `cluster_alias ?? cluster_uuid` (already in cells), `orderBy: 'cluster'` column def (already declared sortable).
- Produces: verified visible sortable "Management cluster" farm column in the ungrouped (overhead) view; code comment noting UUID-text sort groups farms correctly until #692 (no user-facing copy promising names).

- [ ] **Step 1: Write the failing test (only if coverage missing — check first)**

```tsx
// ungrouped rows render the management-cluster cell from cluster_alias with cluster_uuid fallback;
// column carries orderBy 'cluster' (UUID-text sort groups farms until #692)
```
// This test may already pass on current code — the column and its orderBy
// exist today. If so, record Task 3 as verify-only after running it.
```

- [ ] **Step 2: Run test to verify it fails (or passes already — then this task is verify-only)**

Run: `npx jest hcpTableUtils --no-cache`
Expected: FAIL if coverage missing, else PASS (record verify-only and skip to Step 5 with no code change)

- [ ] **Step 3: Implement only the delta (column cell, comment, or nothing)**

- [ ] **Step 4: Run tests to verify they pass**

Run: `npx jest hcpTableUtils optimizationsHcp --no-cache`
Expected: PASS

- [ ] **Step 5: Commit (only if code changed)**

### Task 4: Translations pipeline + full UI verification

**Files:** (generated catalogs only)

**Interfaces:**
- Consumes: Tasks 1–3 message additions.
- Produces: compiled catalogs; green UI suite.

- [ ] **Step 1: Run the translations pipeline**

Run in `apps/koku-ui-ros`: `npm run translations`
Expected: exit 0, catalogs updated (commit them in the same change)

- [ ] **Step 2: Run the full koku-ui-ros Jest suite**

Run: `npm test -- --no-cache` (workspace script: `jest --no-cache`)
Expected: PASS, zero failures (name any pre-existing failure in the report — a red test scrolled past unmentioned is a falsified report)

- [ ] **Step 3: Typecheck/lint the touched surface**

Run: repo's lint/typecheck for the workspace (check root scripts; at minimum `npx tsc --noEmit -p apps/koku-ui-ros` if configured)
Expected: PASS

- [ ] **Step 4: Commit catalogs + push branch**

```bash
git add apps/koku-ui-ros/src/locales/ && git commit -m "feat(ros-ui): HCP sub-tab translations (#689)" (fold into Task 2 commit if cleaner)
git push
```

### Task 5: Report on #689

- [ ] **Step 1: Post the completion table as a #689 comment** (chat is ephemeral; the issue is the durable record), then present it in chat. Table rows: sub URL state, toggle + wiring, farm column, subtitle/link-outs, projection preserved, translations, full suite, lint/typecheck, push. Every dismissal states its reason (backend work: none in v1 — deferred to #690/#691/#692; grouped-namespace view: deferred to #690; alias readability: deferred to #692).
- [ ] **Step 2: Confirm lab verification path** (SNO on-prem UI serves the ROS app; visual check of both sub-tabs against live mgmt data) or record why skipped.
