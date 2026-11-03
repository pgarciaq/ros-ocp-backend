# #753 OpenShift Data-Read Eligibility — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A role with no OpenShift grant (e.g. settings-only) cannot read infrastructure telemetry or recommendations, while settings routes, explicit wildcards, and cluster→node→project inheritance from affirmative parents keep working.

**Architecture:** One shared eligibility predicate in `internal/rbac` — RBAC enabled AND no global `*` AND no `openshift.*` key at all → data scope is EMPTY — applied at the head of every data-side RBAC helper. Absent-child-inherits-affirmative-parent stays each helper's existing behavior; the predicate only closes the absent-everything fall-through.

**Tech Stack:** Go, echo, GORM/pgx, testcontainers, testify; permissions injected via the middleware pattern proven in #754's `heatmapRBACApp`.

**Spec:** [GH #753](https://github.com/pgarciaq/ros-ocp-backend/issues/753) (V15-03, P1) + the handoff comment's proposed direction + session recon 2026-11-03 (all below verified at HEAD `5dc7b595`; zero drift from reviewed baseline `3061a76f` on the audit's six files).

**Recon-verified facts this plan argues from:**

- Middleware (`internal/api/middleware/rbac.go:84-93`): settings ACLs → `settings.read`/`settings.write` keys; OpenShift ACLs → `openshift.cluster|node|project`; only `cost-management:*` yields global `*`. Settings-only roles carry no `openshift.*` key and no `*`.
- Koku's lattice (`koku/koku/rbac.py:32-36,107-127`) — the issue's decision gate, verified: resources start EMPTY; `openshift.node`/`openshift.project` inherit only when the parent's list is non-empty (`openshift.cluster.read != []`). Absent-grant-is-unrestricted exists only on the ROS side.
- **Eight fall-through sites** (systematic grep for `openshift.*` key consumers, superseding the audit's five): `filterClustersByRBAC` (`handlers_node_recs.go:849`), `fleetSummaryNeedsClusterFilter` (`handlers_fleet.go:27`), `AddRBACFilter` (`internal/rbac/query_builder.go:19`), `ApplyNativeRBAC` (`model/recommendation_set_native.go:676`), `applyNativeNamespaceKeysRBAC` (`model/native_ns_list_keys.go:210`), GPU MIG node check (`handlers_gpu_mig.go:~283`), snapshot cluster check (`handlers_snapshot.go:~295`), namespace-recs native RBAC (`model/namespace_recommendation_set_native.go:~555`). All eight grant unrestricted scope when the relevant key is absent.
- `openshiftNodeRBACScope` (`handlers_node_recs.go:878`) is CORRECT as-is (absent child inherits affirmative parent) — untouched. The fleet heatmap path is covered transitively (it consumes `fleetSummaryNeedsClusterFilter` + `openshiftNodeRBACScope`, both handled).
- `handlers_snapshot.go:311` already treats explicit-empty cluster lists as restricted — precedent for empty-grant semantics.
- No-access response semantics are 200 + empty (`meta.count=0` — the issue's own positive control); no error-semantics or UI/IQE contract change.
- `internal/model` already imports `internal/rbac` (`recommendation_set.go` calls `rbac.AddRBACFilter`) — the predicate can live in `internal/rbac` with no cycle.

## Global Constraints

- Production change across 8 sites + 1 new predicate — commits wait for explicit owner go-ahead (repo approval gate).
- **Pre-commit adversarial review** (auth/RBAC class): the closing comment answers "how could these tests pass while the bug lives?"
- Do NOT deny settings-only identities at middleware — settings routes must stay usable (issue scope gate).
- Preserve: global `*`; explicit `openshift.*: ["*"]`; absent-child-inherits-affirmative-parent; RBAC-disabled mode; 200+empty semantics.
- No drive-by refactors; solve the demonstrated case. Out of scope: #752 (six readers that never consume restrictions — sequenced after this), #789.
- No `t.Skip`, no weakened assertions; rules-out comments per adversarial-fixtures.md.
- Push target: fork (`pgarciaq`), branch `pgarciaq-rosocp-superpowers-phase17`.

## Review Focus

1. **Settings-only read AND write variants** both denied on data surfaces (the write-only variant is equally a data-read hole today). → Task 2 repro matrix.
2. **Explicit-empty grants** (`openshift.cluster: []`) denied — Koku treats empty as no access; verify GORM's `IN (?)` with an empty slice actually denies (empirically, not by assumption). → Task 2 empty-grant test.
3. **Inheritance preserved** — a cluster-granted user with no node key still sees nodes within their cluster scope; a node-granted user retains node-level access. → Task 2 inheritance tests.
4. **Settings routes stay usable** for settings-only roles (positive control against over-denial). → Task 2 settings-positive test.
5. **No ninth site** — the sweep is re-run as a verification step post-implementation; any new `openshift.*` consumer added meanwhile must route through the predicate. → Task 4.

---

### Task 1: The eligibility predicate (self-contained TDD)

**Files:**
- Create: `internal/rbac/eligibility.go`
- Test: `internal/rbac/eligibility_test.go`

**Interfaces:**
- Produces: `OpenShiftDataAccessEligible(userPerms map[string][]string) bool` — true when RBAC disabled, global `*` present, or ANY of `openshift.cluster|node|project` is present as a key (even an explicitly empty list — per-key filtering already denies those). False only when RBAC is enabled, no `*`, and no `openshift.*` key exists.

- [ ] **Step 1: Write the failing table test** — cases: settings-read-only → false; settings-write-only → false; settings both → false; empty map → false; global `*` → true; cluster list → true; node-only → true; project-only → true; explicit-empty cluster list → true (eligibility; filtering denies); RBAC disabled (`t.Setenv("RBAC_ENABLE","false")` + `config.ResetForTest()`) → true.
- [ ] **Step 2: Run → RED** (`undefined: OpenShiftDataAccessEligible`).
- [ ] **Step 3: Implement** — the boolean described in Interfaces; doc comment cites Koku's `rbac.py` lattice and #753.
- [ ] **Step 4: Run → GREEN.** Commit (after owner go-ahead): `feat(#753): openshift data-read eligibility predicate — absent grants are not unrestricted`.

### Task 2: Regression tests + api-layer sites (RED on baseline → GREEN)

**Files:**
- Modify: `internal/api/handlers_node_recs.go` (filterClustersByRBAC), `internal/api/handlers_fleet.go` (fleetSummaryNeedsClusterFilter), `internal/api/handlers_gpu_mig.go`, `internal/api/handlers_snapshot.go`
- Create: `internal/api/rbac_eligibility_regression_test.go`

**Interfaces:**
- Consumes: `rbac.OpenShiftDataAccessEligible` (Task 1).
- Produces: test app builder `newPermsApp(t, perms)` (generalizing #754's `heatmapRBACApp` middleware pattern to arbitrary routes); seed helper for the #753 repro (cluster B `hourly_node_digests` with CPU 617 / memory 123456 / max_pod_count 42, plus cluster A).

- [ ] **Step 1: Write the failing tests** (handler-level, through real middleware):
  - Primary repro (the issue's): settings-read-only user GETs node hourly-utilization → must be `meta.count=0` (baseline: 1 with B's telemetry values). Same for settings-write-only.
  - Fan-out: settings-only on fleet summary (zeros), fleet heatmap (count 0), native container list, namespace list (these two may need Task 3's model-layer fix — split assertions accordingly or move to Task 3's tests), compat container list, GPU MIG, snapshot list.
  - Positive controls: A-only grant denies B (pin existing behavior); wildcard sees all; cluster-granted-no-node-key sees nodes in scope (inheritance); settings-only CAN read settings routes (e.g. terms — through the settings RBAC test app pattern).
  - Explicit-empty `openshift.cluster: []` → denied (verify GORM empty-IN behavior empirically here).
- [ ] **Step 2: Run → RED on baseline** for every settings-only data assertion, with mechanism-naming messages.
- [ ] **Step 3: Apply the predicate at the four api-layer sites** — `filterClustersByRBAC`: ineligible → return nil; `fleetSummaryNeedsClusterFilter`: ineligible → return true (routes through cluster resolution → empty set → existing empty early-return; also gives settings-only users their own `org:rbac:<hash>` cache key, no shared-entry pollution); GPU MIG node check: `!hasNode` → return eligibility; snapshot cluster check: `!hasCluster` → ineligible returns the restricted-empty shape (mirror the existing `len==0` branch at :311).
- [ ] **Step 4: Run → GREEN** (api-layer assertions; model-layer ones may still be red — Task 3). Commit (after owner go-ahead): `fix(#753): api-layer data helpers deny absent-grant identities`.

### Task 3: Model/rbac-layer sites

**Files:**
- Modify: `internal/rbac/query_builder.go` (AddRBACFilter), `internal/model/recommendation_set_native.go` (ApplyNativeRBAC), `internal/model/native_ns_list_keys.go`, `internal/model/namespace_recommendation_set_native.go`
- Test: extend Task 2's regression file (or `internal/model` tests where the helper is unit-testable directly).

- [ ] **Step 1: Failing tests** for the four query builders — settings-only perms produce zero rows through the native container list, namespace list/keys, namespace recs, and compat list (AddRBACFilter path).
- [ ] **Step 2: RED on baseline.**
- [ ] **Step 3: Apply** — each builder: after the RBAC-disabled/`*` checks, `if !rbac.OpenShiftDataAccessEligible(userPerms) { return query.Where("1 = 0") }` (model files already import `internal/rbac`; query_builder lives in it).
- [ ] **Step 4: GREEN.** Commit (after owner go-ahead): `fix(#753): native and compat query builders deny absent-grant identities`.

### Task 4: Mutation check + adversarial pass + full verification

- [ ] **Step 1: Mutation** — remove the eligibility check from `filterClustersByRBAC` only → the primary repro fails; remove from one model builder → its test fails. Restore.
- [ ] **Step 2: Adversarial controls** — reversed grant orders, same-value fixtures, the A-denies-B control re-verified post-fix, settings-positive re-verified post-fix.
- [ ] **Step 3: Sweep re-run** — the `openshift.*` consumer grep returns the same eight sites + the predicate; no ninth unguarded site.
- [ ] **Step 4: Full suite** — `make test` → PASS (headline claim; no success reported without this output).
- [ ] **Step 5: Commit any strengthening** (after owner go-ahead).

### Task 5: Close-out

- [ ] **Step 1: Comment on #753** — findings, the eight-site inventory (superseding the audit's five), mutation results, the adversarial answer, #752 sequencing note (readers wired through the now-trustworthy helpers as its own issue).
- [ ] **Step 2: Close with the completion-report table** — verified vs asserted (CI on pushed commits; iqe-ros-ocp-plugin settings-only coverage noted as a cross-repo follow-up per the handoff table).

---

## Self-review notes

- **Spec coverage:** criterion 1 (settings read/write-only denied) → Task 2 matrix; criterion 2 (settings routes usable) → settings-positive control; criterion 3 (inheritance/wildcards preserved) → Task 2 controls + predicate semantics; criterion 4 (all fan-out surfaces + A-denies-B control) → eight-site inventory, Tasks 2-3; criterion 5 (RED on baseline, right reason, correction, adversarial controls) → Tasks 2-4; criterion 6 (suites, executed vs skipped) → Task 4; criteria 7-8 → Task 5.
- **Proportion:** one new predicate + eight one-to-four-line site changes + one regression test file; no API shape, error-semantics, or cache-key dimension changes (settings-only users gain their own `org:rbac:<hash>` entries via the existing fleet helper semantics).
