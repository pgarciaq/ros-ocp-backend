# #797 — Org-Scoped Alias Resolution in parseClusterParams — Implementation Plan

> The fix direction is recorded on the issue (mirror #790's
> `clusterFilterPredicate`); no owner decision gate. This plan records the
> recon-verified surface and the mechanism.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`.
Tracker: #857 (deferred/owner-level section — now the active item).

## The verified surface (recon 2026-11-18)

The issue says "seven endpoints"; the code funnel is narrower and
cleaner. `parseClusterParams` resolves aliases through the org-unscoped
subquery **only when `uuidColumn != ""`** (the container-compat shape):

| Path | Column | Alias branch | Vulnerable? |
|---|---|---|---|
| `MapQueryParameters` → `GetRecommendationSetList` → `/recommendations/openshift` (non-plugin), `/container` | `recommendation_sets.cluster_uuid` | subquery | **YES** |
| `MapNativeCompatQueryParameters` → `GetNativeCompatRecommendationSetList` → `/container` (plugin) | same | subquery | **YES** |
| `handlers_hcp.go` (reuses `MapQueryParameters`) → HCP list | same | subquery | **YES** |
| `MapNamespaceQueryParameters` → namespace list | `""` | direct `clusters.cluster_alias` (outer query) | outer join — #525-swept; verified by the regression (expected control) |
| History / quality (`c.cluster_alias IN ?`) | direct | outer join | not parseClusterParams; #525-swept |
| Native lists (`parseNativeClusterParams`, `c.cluster_alias`) | direct | outer join | not parseClusterParams |

The regression covers the compat container list (the RED) and the
namespace list (per the issue's close criteria — expected safe via the
org-scoped join; if the RED shows otherwise, STOP and surface: the fix
would be bigger than this plan).

## Mechanism

Thread the caller's org down the pure-function chain:

`applyParamFilter(c, …)` — extracts orgID from the identity context
(non-erroring: `c.Get("Identity")` → `OrgID`, else `""`) →
`buildSQLClauseWithFilterType` → `buildModeClause` → `parseClusterParams`.

`parseClusterParams` adds `org_id = ?` to BOTH subqueries (include and
exclude branches), with orgID bound FIRST:

```sql
IN (SELECT cluster_uuid FROM clusters WHERE org_id = ? AND cluster_alias …)
```

- Within-org semantics unchanged (the org predicate is a no-op for
  own-tenant aliases — the subquery returns the same UUIDs).
- UUID-form filters untouched (no subquery).
- Empty orgID (identity-less unit-test contexts) fails CLOSED: the
  subquery matches nothing. Safe direction; the existing bare-context
  clause tests use the direct branch (column `""`) or UUID form, so
  they are unaffected.
- No ILIKE-semantics change (the issue's optional parity sentence is a
  no-op: include mode already modeClause-wraps; only the org predicate
  is missing — scope to the demonstrated case).

`buildClauseForParam` (the api_test.go helper) re-signs with orgID.

## Tests (RED first)

1. **The RED — the colliding-UUID oracle on the compat container list:**
   the #552 shape (cluster B's UUID registered under a second tenant
   with `foreign-tenant-alias`); `filter[cluster]=foreign-tenant-alias`
   on `/recommendations/openshift/container` must return EMPTY.
   Baseline: the unscoped subquery resolves the shared UUID and returns
   this org's row for it — the leak.
2. **The namespace list, same fixture** — must return EMPTY (expected
   control: the direct branch's outer join is org-scoped; if the
   baseline fails here, stop and surface).
3. **Controls (must hold before AND after):** our own alias
   (`beta-b`) resolves our cluster B's rows on both routes (own-org
   resolution intact, and the tests non-vacuous); UUID form unchanged.
4. **Clause-map tests:** the subquery branch gains the org predicate
   and param (org bound first) — extends `TestBuildSQLClauseWithFilterType`.

## Mutations planned

- **M1** (org predicate removed): the RED returns.
- **M2** (org/alias param order swapped): the controls fail — the
  subquery binds the alias as the org — pinning the binding order.

## Deliverables

- `internal/api/utils.go` — the org threading + the two subquery
  predicates; `internal/api/api_test.go` — the helper re-sign + the
  clause-map cases.
- `internal/api/cluster_alias_filter_regression_test.go` — the
  colliding-UUID compat-path regression (routes + seed + controls).
- CHANGELOG (+ pair); completion report per properly-implement-feature.

**Non-goals:** ILIKE-semantics changes; the direct-alias branches
(outer joins — already swept); history/quality paths; the native
lists.

## Fresh-context review outcomes (2026-11-18, verified before action)

Verdict: the fix is correct and complete for the demonstrated channel;
threading sound and fail-closed; mutation coverage genuine. Nothing
blocks. Dispositions:

- **P2 (the collision insert is unpinned — the RED could go
  green-vacuous if a future fixture edit makes the ON CONFLICT a
  no-op): FIXED** — both collision tests (#790's and this cycle's) now
  assert the foreign-tenant row exists (count = 1) after the seed.
- **P3 (exclude-mode behavior change unrecorded): FIXED** — the
  CHANGELOG now names the exclude-side channel (the other tenant's
  alias used to REMOVE this org's shared-UUID rows; it no longer does)
  so a future diff does not "restore" it.
- **P3 (surface imprecision): FIXED** — the CHANGELOG now scopes
  `/recommendations/openshift` to plugin mode (native mode serves the
  fallback → direct branch, not vulnerable); `/container` affected in
  both modes.
- **P3 (fixture lessons unrecorded): FIXED** — the seed comment now
  records all three invariants (the #607 short/cost pin, the
  COUNT(DISTINCT container_id) NULL-ignore, the same-second
  date-window trap).
- **P3 (test-app wiring note): FIXED** — aliasFilterCompatApp's comment
  records that the registered handler differs from the production
  wiring of /container while sharing the mapper.
- **P3 (plan-text corrections, recorded here):** the subquery
  clause-map assertions landed in TestParseClusterParams_OuterColumn
  (TestBuildSQLClauseWithFilterType's cluster cases all use the direct
  branch); HCP is covered transitively through the shared mapper (M1
  is caught by the container test), not by a route-level regression of
  its own.
- **Noted:** the regression file is DB-backed — the RED runs only
  under make test / Docker (SetupTestDB skips in -short); the
  completion report says so explicitly. The exclude/exact route-level
  RED remains include-only (the clause-map layer pins all three modes).

The reviewer's exhaustive alias-site audit found every other
resolution site already org-scoped (clusterFilterPredicate,
resolveClusterFilterUUID, the native c.* joins, the namespace LEFT
JOIN, the farm-column scalar subquery, HCP EXISTS, business-hours,
clustercache, savings-summary) — no residual channel.
