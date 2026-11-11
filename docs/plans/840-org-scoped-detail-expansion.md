# #840 Cross-Tenant Detail Expansion — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner go-ahead 2026-11-11: fix the branch-review P1 blocker. Tenant isolation —
> the AGENTS.md pre-commit-review class applies: a reviewer answers "how could
> these tests pass while the bug lives?"

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `f3db9a38`. Tracked as #840 (branch-review finding 1, tracker #849).

**Recon facts (all verified in code):**
- The defect site: the container-list keys-path detail expansion
  (`internal/model/recommendation_set_native.go:598–610`) joins
  `recommendation_sets rs` through `JOIN clusters c ON c.cluster_uuid =
  rs.cluster_uuid AND c.org_id = ?` — constraining the CLUSTER row's org,
  never `rs.org_id`. The page subquery is org-scoped (ock.org_id), but its
  join to rs is by IDENTITY ONLY — org B's rs rows with the same cluster
  UUID + identity match.
- The schema permits the shape: `cluster_uuid` is NOT globally unique
  (migration 191 states it; the clusters unique key is
  (tenant_id, source_id, cluster_uuid, cluster_alias)) and `org_id` is in
  the recommendation_sets PK (migration 000028).
- The file's own header states the invariant: "Every detail lookup MUST
  filter by org_id (recommendation_sets.org_id) so one tenant cannot read
  another tenant's data by guessing a UUID"
  (recommendation_set_native.go:104–106).
- Mechanism-class survey (corrected during implementation): EVERY other
  `JOIN clusters` site in internal/model pairs the join with
  `WHERE <alias>.org_id = ?` — namespace (ns.org_id ×6),
  org_namespace_keys (onk.org_id ×2), quality tables (q.org_id ×6 incl.
  PVC/GPU-MIG), history (h.org_id ×2), and the same file's single-record
  detail builder. TWO sites were violations, not one: the keys-path
  detail expansion (~520) AND the DISTINCT-fallback detail expansion
  (~613) — the latter is currently unreachable
  (`usesOrgContainerKeys()` returns true unconditionally, ADR-0306) but
  carries the same identity-only re-join and is fixed as class
  insurance. The correct join pattern exists in-file:
  `nativeContainerRSJoin` (line 377) joins `rs.org_id = ock.org_id`.
- Sol's executed read-only CTE demonstrated the leak (A-scoped page
  returning B's `long|performance` row).

**Mechanism:** add `Where("rs.org_id = ?", orgID)` to the keys-path
detail query — matching the file's own rule, its other sites, and the
class survey. No other site needs the fix (survey above); the RED
asserts the class by construction (the same test would fail on any
equally-unconstrained site exercised through the list path).

**Tests:**
- RED: `TestGetNativeRecommendations_DetailExpansionScopesByOrgID` —
  org A's real pipeline (digests → RecommendAllWorkloads →
  WriteRecommendationsAndRefreshOrg) produces A's full term/engine
  matrix; org B registers the SAME cluster UUID (own tenant, per
  migration 191) and writes a rogue rs row through the PRODUCTION
  writer with the same container identity and term="rogue" — a term A's
  pipeline never produces, so the `rogue_term` key appearing in A's
  result IS the leak: unambiguous and order-independent (two earlier
  assertion drafts were invalid — a term-absence check false-positived
  on A's own long_term, and a value check was order-dependent and
  passed on baseline; both caught by the stash-rerun discipline).
  A's own `short_term`/cost variant must be present (control).
- Baseline expectation (verified by stash-rerun): `rogue_term` APPEARS
  in A's result on the unfixed tree.
- Mutations: the org constraint removed → RED fails; the constraint
  inverted (`!=`) → the control fails (A's own variant vanishes);
  constraint on the WRONG alias (e.g., c.org_id only — the original
  bug) → RED fails. Stash-rerun.
- The tenant-isolation pre-commit question (how did the bug live under
  a green suite?): `TestDetailQueries_ScopeByOrgID` covers
  single-record detail builders, not the list expansion — the answer
  goes in the completion report and the fresh-context review.

**Non-goals:** findings #841–#848 (their own issues); no refactor of
the join architecture; no multi-org seeding of the full suite.
