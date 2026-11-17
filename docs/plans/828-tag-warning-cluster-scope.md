# #828 Tag-Warning Cluster Scope — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner approved the scope 2026-11-09: a **conformance fix, no policy change** —
> the Koku-aligned two-source policy #752 already ledgered. Implementation in
> ros-ocp-backend.

**Goal:** a cluster-restricted caller must receive the same unknown-key warning
treatment for a tag key observed only on an inaccessible cluster as for a
nonexistent key — on every route that attaches tag warnings.

**Policy (unchanged, now with Koku parity as rationale):** the warning's "known"
set has two sources, matching Koku's two mechanisms:
1. **Provider catalog (enabled keys) stays org-wide** — Koku's own validation
   (`api/query_params.py:_set_tag_keys`) checks requested tag keys against
   `EnabledTagKeys` with no access scoping; org-enabled status is deliberately
   org-visible there.
2. **Data-derived keys (`resolved_tags` scan) scope to the caller's
   server-owned cluster set** — Koku's tag queries scope observed data by
   access (`api/tags/ocp/queries.py`); ROS must never reveal where a key is
   observed outside the caller's scope.

**The two defects (verified by the V16 triage probe):**
- Five caller groups pass `nil` scope to the attach helpers: node-utilization
  (`handlers_node_utilization.go:551`), GPU MIG list + empty-response
  (`handlers_gpu_mig.go:194,215`), history (`handlers_history.go:212`), native
  container (`handlers.go:536` + `serveNativeList` :707), namespace
  (`serveNativeNamespaceList` :858,873).
- **Nil-vs-empty conflation:** `filterClustersByRBAC` returns `nil` for denied
  identities (settings-only, empty grants); `tagWarningScope` passes that
  `nil` through; `knownTagKeysForOrg` treats `nil` as *unscoped* — org-wide.
  This defeats even the scoped PVC path for denied users (reproduced).

**Mechanism:**
1. `tagWarningScopeResolver` — a lazy `func(ctx) (scope []string, err error)`:
   `nil, nil` = unscoped (org-wide data-derived); a non-nil slice (including
   EMPTY) = scoped to exactly those clusters; error = suppress warnings (the
   scope cannot be established, so no existence-revealing diagnostic fires —
   fail-silent in the safe direction).
   - `staticTagWarningScope(scoped, allowed)` — for callers that already hold
     the RBAC-filtered list (PVC, node-util, GPU MIG main path): zero extra
     queries, and materializes `[]string{}` for scoped-nil (the denied case).
   - `requestTagWarningScope(c, orgID)` — for callers without a materialized
     list (history, native container, namespace, GPU MIG empty-response):
     resolves via `fleetSummaryNeedsClusterFilter` + `getClustersForOrg` +
     `filterClustersByRBAC`, **lazily** — `buildTagWarnings` calls the resolver
     only after its early-outs (results present / no tag filters / feature
     off), so the happy path pays no extra query.
2. `buildTagWarnings` + the four attach helpers take the resolver; all eight
   unscoped call sites are wired. PVC's behavior is otherwise unchanged
   (`staticTagWarningScope` over its existing `dataClusterScope` result).
3. GPU MIG main path captures the RBAC-filtered set BEFORE the client cluster
   filter composes (client filters narrow data, never the warning scope).

**Tests (adversarial-fixtures discipline):**
- RED matrix: for each route (node, mig, mig-empty, history, native-container,
  namespace, pvc-denied, pvc-settings-only), a restricted caller gets the SAME
  warning treatment for a B-only key and a nonexistent key. All fail on
  baseline (the V16-01 triage probe is the seed).
- Controls that pass pre AND post: PVC A-only (already scoped); wildcard
  (B-only key known, nonexistent warns — correct for an org-authorized user);
  authorized-key (an A-side observed key produces no unknown-key warning);
  RBAC-disabled path via the unscoped resolver branch.
- Mutations: (a) revert the empty-materialization → denied cases fail;
  (b) revert one caller to nil → its route fails; (c) resolver error →
  warnings suppressed.
- Stash-rerun of the full matrix against the original code.

**Docs:** issue comment on #828 recording the Koku-parity rationale and the
two-source policy; CHANGELOG Fixed entry. The project/node-grain gap (Koku
scopes observed tags by project and node too; ROS warnings stay cluster-grain)
is a **separate follow-up issue**, not built here.

**Non-goals:** no provider-catalog scoping (org-wide by design, Koku parity);
no project/node-grain scoping (follow-up); no warning-text change ("not in the
known tag catalog" remains accurate); no new warning coverage on routes that
never warned (e.g. grouped node responses — absence of warnings cannot leak).
