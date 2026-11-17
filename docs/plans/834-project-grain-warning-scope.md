# #834 — Approach A: Project-Grain Tag-Warning Scope — Implementation Plan

> **Decision (owner, 2026-11-17):** approach A — match Koku's project
> grain in the warning catalog's data-derived half; document node grain
> as the remaining limitation (a data-model constraint:
> `org_container_keys` has no node dimension — a forced boundary, not a
> chosen one). Rationale: #828's "never reveal where a key is observed
> outside the caller's scope" completes at project grain; the warning
> annotates list results that are already project-scoped
> (`applyProjectFilter`); the remaining boundary becomes a constraint.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`.
Tracker: #857 (the last ros-ocp-backend item).

## The exposure being closed

The warning's known-set = org-enabled catalog (org-wide BY the 2026-11-09
decision — unchanged) ∪ data-derived keys (cluster-scoped since #828).
The data-derived half is coarser than the results it annotates: a user
restricted to project `ns-rs` on cluster A currently learns that a
non-enabled key observed in cluster A's OTHER namespaces exists (no
warning) vs a nonexistent key (warning) — the same existence-oracle class
#828 closed across clusters, now within one cluster across projects.

## Mechanism

Mirror `rbac/query_builder.go`'s project semantics exactly — no new
policy, no invented shapes:

1. **`tagWarningScope` struct** `{ clusters, namespaces []string }` —
   per-dimension nil = unscoped, empty = scoped-to-nothing (the #828
   nil-vs-empty discipline carried to the second dimension). The
   resolver signature returns it.
2. **`projectNamespaceScope(userPerms)`**: RBAC off, `*` perms, absent
   project key, or `*` in project perms → nil (unscoped); otherwise the
   grant list as-is — explicit (scoped) or empty (Koku: an empty grant
   is no access → scoped to nothing).
3. **`requestTagWarningScope`**: the unscoped short-circuit becomes
   `!clusterScoped && ns == nil`. A cluster-unrestricted but
   project-restricted user (e.g. `openshift.cluster: *` +
   `openshift.project: [ns-rs]`) gets the namespace filter with NO
   cluster query; a cluster-scoped user gets both; a project-only user
   inherits all clusters (filterClustersByRBAC's existing
   absent-parent semantics) with the namespace filter.
4. **`staticTagWarningScope(userPerms, clusterScoped, allowedClusters)`**:
   signature gains the perms; namespaces derive inside and apply
   regardless of the cluster-scoped flag (the static sites: PVC,
   node-utilization, GPU MIG).
5. **`knownTagKeysForOrg(ctx, pool, orgID, scope)`**: the key query adds
   `AND namespace = ANY($n)` when namespaces is non-nil — riding the
   same cluster-filtered scan, one more bind parameter, no new query.
6. **`buildTagWarnings`** passes the scope struct through.

Zero new queries anywhere; the lazy design (past the early-outs) is
unchanged; happy paths pay nothing.

## Tests (all RED-verified at HEAD first)

1. **The oracle (the RED):** reader with `openshift.cluster: [A]` +
   `openshift.project: [ns-rs]`; secret key seeded on cluster A's
   `other-ns-rs`; the secret key and a nonexistent key must BOTH warn —
   indistinguishable. Run on one request-scope route (containers) AND
   one static-scope route (PVCs) — both resolver shapes.
2. **Controls (must hold before AND after):** the authorized key
   (ns-rs) does not warn; a project-unrestricted user (no project
   grants) sees the secret key as known; a wildcard user sees it known.
3. **The gate case:** `openshift.cluster: *` + project-restricted —
   the secret key still warns (namespace applies when
   cluster-unscoped).
4. **The nil-vs-empty pin:** `openshift.project: {}` + cluster grant —
   scoped to nothing, both keys warn (an empty grant is no access).
5. **Existing suites hold:** the #828 cluster matrix unchanged.

## Mutations planned

- **M1** (namespace predicate dropped): the oracle returns.
- **M2** (empty namespaces read as unscoped — the nil-vs-empty
  conflation): test 4 fails.
- **M3** (the gate reverts to cluster-only): test 3 fails.

## Deliverables

- `internal/api/tag_warnings.go` — the scope struct, the namespace
  derivation, the query predicate.
- The three static call sites (`handlers_pvc.go`,
  `handlers_node_utilization.go`, `handlers_gpu_mig.go`) — pass perms.
- `internal/api/tag_warning_scope_regression_test.go` — the project
  matrix + controls (seed helper: same-cluster two-namespace world).
- The node-grain limitation recorded: issue comment + the resolver's
  contract comment.
- CHANGELOG (+ pair); completion report per properly-implement-feature.

**Non-goals:** node grain (not representable — no node dimension in
`org_container_keys`); the org-wide enabled-key catalog (settled
2026-11-09, Koku parity); any change to list-result scoping (already
project-scoped).

## Fresh-context review outcomes (2026-11-18, verified before action)

Verdict: sound — no P1 or P2. The derivation is condition-for-condition
faithful to the query builder across every perm shape walked; the
nil-vs-empty discipline is airtight in both dimensions and both failure
directions; the SQL placeholder numbering is correct in all four
combinations (the namespaces-only shape is exercised by the gate test);
the mutation check independently reproduced M1–M3 with the same
mechanism-naming failures.

**Premise correction (the review's sharpest catch):** this plan's
rationale — "the warning annotates list results that are already
project-scoped" — is true for the request-scope routes (containers,
namespaces, history) and FALSE for the three static sites: the PVC,
node-utilization, and GPU MIG data paths filter by cluster (and node)
but not by project. Verified in source before accepting
(handlers_pvc_filters.go binds only the client project param;
node-utilization filters cluster+node; GPU MIG cluster+client). The
consequence of approach A on those routes is over-warning only — the
catalog is now FINER than the results (a finer catalog reveals strictly
less; no leak), but the warning text is arguably misleading there (the
key's data is visible in the caller's own unfiltered list). The
underlying cause is the pre-existing data-path gap — filed separately,
not folded here.

Dispositions:
- **P3 (eligibility-skip invariant): documented** in
  projectNamespaceScope's comment — the skip is safe only because every
  caller derives clusterScoped from fleetSummaryNeedsClusterFilter /
  dataClusterScope; a future caller passing clusterScoped=false for an
  ineligible project-absent identity would resurrect the #828 class.
- **P3 (catalog-finer-than-results divergence): documented** in
  knownTagKeysForOrg's comment, the CHANGELOG entry, and this plan; the
  data-path gap filed as a separate question issue.
- **P3 (test gap): closed** — the project matrix now runs all four
  routes (containers-fallback, pvcs, nodes, mig), covering every
  re-signed static site.
- **P3 nit (placeholder asymmetry): fixed** — both AND clauses compute
  their placeholder from the args built so far, with the append-order
  invariant commented.
- **P3 nit (two get_user_permissions(c) lookups in handlers_pvc.go):
  left as-is** — verified safe (per-request copies from the same
  context, pinned by middleware/rbac_cache_test.go); hoisting would
  touch the data path for no behavior change.
- **P3 (RBAC-disabled project case): left untested** — unreachable in
  this suite (setupEligibilityTest forces RBAC_ENABLE=true); noted by
  the reviewer as minor.
