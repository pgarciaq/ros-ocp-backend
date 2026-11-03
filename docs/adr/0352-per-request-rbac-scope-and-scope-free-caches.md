# ADR-0352: RBAC Data-Read Scope Is a Per-Request Concern; Caches Hold Scope-Free Org-Level Data

## Status

Accepted — implemented in #753 (`f2515df0`) and #754 (`6bfb4c98`)

## Phase

API security — data-helper authorization and cache scoping

## Context

Two P1 findings from the v15 adversarial review (2026-11-01) exposed the
same architectural flaw from two sides:

- **V15-03 (#753):** data-side RBAC helpers treated an *absent* OpenShift
  grant as unrestricted access. A settings-only role
  (`cost-management:settings:read` or `:write`) carried no `openshift.*`
  key and no global `*`, and every helper's fall-through (`!hasCluster →
  unrestricted`) served it org-wide telemetry and recommendations. Koku's
  independent permission lattice (`koku/koku/rbac.py`) starts resources
  **empty** and grants inheritance only from an affirmative parent
  (`openshift.cluster.read != []`) — the absent-grant semantics existed
  only on the ROS side.

- **V15-04 (#754):** the fleet heatmap cached the fully-built,
  node-filtered response under a key carrying only the cluster dimension.
  A wildcard-cluster role with a narrow `openshift.node` grant shared the
  `org:all` entry with an unrestricted role, and a cache hit returned
  before the node filter — leaking secret nodes in one warm order and
  serving poisoned partial views in the other.

The verification round added an uncomfortable fact: the existing test
suite had been *embodying* the bug — one fixture's "authorized" reader
used nil permissions under RBAC left enabled by its own earlier phase,
and eight more tests passed for years atop a config-singleton leak that
the absent-grant semantics kept invisible.

## Decision

1. **Eligibility is affirmative.** `rbac.OpenShiftDataAccessEligible` is
   the single gate: RBAC disabled or a global `*` passes; an
   `openshift.cluster|node|project` key with a **non-empty** list is
   eligible; anything else — absent keys, or keys the middleware left
   explicitly empty — has an empty data scope. Empty grants deny because
   the container/namespace builders do not filter on node grants, so key
   presence alone must never authorize. Absent *child* grants still
   inherit an affirmative parent's scope (each helper's existing
   behavior, mirroring Koku).

2. **Enforcement lives in the data helpers, on every request.** The
   predicate runs at the head of every data-side RBAC helper — cold and
   warm paths alike. Middleware-level denial was explicitly rejected:
   the RBAC middleware admits settings ACLs, and settings-only
   identities must keep settings-route access. Authorization of data is
   a query-shaping concern, not a routing concern.

3. **Caches store scope-free org-level payloads.** The fleet heatmap
   caches the pre-truncation org-level node pool under the unchanged key
   dimensions (org, cluster-scope discriminator, client cluster filter,
   term, engine, metric); per-user rendering (node filter into a NEW
   slice, maxNodes truncation, Count, warnings) happens post-cache on
   every request. No cached artifact ever bakes per-user state.

4. **New data routes must consume the helpers.** Any handler that reads
   tenant data joins the helper contract (`filterClustersByRBAC`,
   `ApplyNativeRBAC`, `AddRBACFilter`, …) or applies the predicate
   directly. Three routes that had silently bypassed every helper
   (snapshot cost-by-type, snapshot age-distribution, cluster
   instance-types) were scoped under this rule in #753; #752 tracks the
   remaining known readers.

## Alternatives Considered

- **Deny settings-only identities at middleware** — rejected: breaks
  legitimate settings access, and couples route admission to data
  authorization (the issue's own scope gate).
- **Node scope in the cache key** — rejected for the heatmap: per-grant
  key fragmentation with unbounded cardinality, for no benefit when the
  response has no cross-node aggregates to recompute.
- **Per-helper inline absent checks** — rejected: eight copies of one
  semantic to keep in sync; the predicate is the single point of truth.

## Consequences

- No-access is **200 + empty**, not 403 — the established pattern; no
  error-semantics or UI/IQE contract change.
- Engine-level filter builders that treat a zero-length cluster slice as
  *no filter* (`len(x) > 0` guards) require empty-set early returns at
  the handler — the GPU MIG and grouped savings paths carry them; this
  hazard is now a review checklist item for new list endpoints.
- Test fixtures must express affirmative grants for "authorized"
  readers, and tests that `t.Setenv` RBAC-affecting variables must
  `t.Cleanup(config.ResetForTest)` — the deny path makes config-singleton
  leakage visible as order-dependent failures.
- `AddRBACFilter` applies clauses through GORM's in-place statement
  mutation (its existing contract); Session-cloned inputs are not a
  supported caller shape.

## Verification

- #753: primary repro (node-hourly telemetry) RED on baseline → GREEN;
  full `make test` green; four mutation checks (two per layer) detected;
  real-middleware chain test (mocked RBAC service through
  `ros_middleware.Rbac`) proves deny + settings-read positive +
  settings-write positive.
- #754: both warm orders hit-proofed (post-warm DB deletion proves the
  cache served the data); cold/warm parity; two-cluster same-name
  isolation by UUID; filter-before-truncate cap semantics; mutations for
  the node filter and the cache write.

## Related Decisions

- #789 tracks the open product question of whether fleet/savings summary
  *aggregates* should be node-scoped (no filter exists there today — a
  separate question from this ADR's mechanism).
- #752 tracks the six readers (PVC, quota, cluster-quota, HCP
  advisories, OOM timeline) still to be wired through the helpers this
  ADR makes trustworthy.
