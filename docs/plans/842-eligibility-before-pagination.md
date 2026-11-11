# #842 Detail-Only Filters Before Pagination — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner go-ahead 2026-11-11 (the queue: #842 → #841 → #843 → #844, per
> docs/plans/841-844-list-architecture-queue.md). This cycle builds the
> eligibility infrastructure; #841's reclassification then inherits it.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `ca336610`. Tracked as #842 (branch-review finding 3, tracker #849).

**Recon facts (verified; see the queue plan for the shared machinery):**
- Both keys paths (container `getNativeRecommendationsFromOrgKeys`,
  namespace `getNativeNamespaceRecommendationsFromOrgKeys`) run COUNT and
  PAGE-KEYS on the keys table with ONLY keys-leg predicates; detail-only
  predicates (`rs.term/idle_state/has_gpu/gpu_classification/gpu_idle_state/updated_at`,
  `ns.term/engine/idle_state/stale/monitoring_end_time`) apply AFTER the
  page is selected.
- Consequences (Sol's executed CTE: `reported_count=3 | returned_rows=0`,
  `HasNext=false`): counts omit the predicates; a page of nonmatches
  swallows the limit and `HasNext` — computed from survivors — reports no
  continuation while matches remain beyond the selected keys.
- The container DISTINCT fallback applies full queryParams to its
  rs-based subquery — eligibility correct there; the defect is
  keys-path-specific.
- Historical interaction: `31591568` added namespace term filtering
  pre-key-table; `184bd61d` introduced the page-selection leg without
  carrying those predicates into eligibility.

**Mechanism:** when detailParams is non-empty, apply a recommendation-
eligibility `EXISTS` to BOTH the count and the page-keys queries:
- Container: `EXISTS (SELECT 1 FROM recommendation_sets rs WHERE
  rs.org_id = ock.org_id AND rs.cluster_uuid = ock.cluster_uuid AND
  rs.namespace = ock.namespace AND rs.workload = ock.workload AND
  rs.workload_type = ock.workload_type AND rs.container_name =
  ock.container_name AND <detail predicates>)` — the same identity shape
  as `nativeContainerRSJoin`.
- Namespace: `EXISTS (SELECT 1 FROM namespace_recommendation_sets ns
  WHERE ns.org_id = onk.org_id AND ns.cluster_uuid = onk.cluster_uuid
  AND ns.namespace_name = onk.namespace_name AND <detail predicates>)`
  — the same shape as `nativeNSKeysNRSJoin` (plus `ns.term IS NOT NULL`,
  matching the detail query's shape).
- Cost: the EXISTS runs whenever detail-only predicates are present —
  which INCLUDES default API requests (they carry date-range
  predicates; review correction of this plan's earlier "pays nothing"
  claim). The identity join is PK-index-backed (verified); rs.stale /
  ns.stale are excluded from eligibility as redundant with the keys
  legs' stale filters; cost at org scale is unverified (next
  verification: EXPLAIN ANALYZE on an org-scale fixture).
- The detail predicates STAY on the detail query (the EXISTS checks
  that ANY matching row exists; the detail join still filters rows).

**Tests:**
- RED (container): three identities sorted a,b,c — a short-term only,
  b and c long-term only (hand-built recs through the production writer
  + RefreshOrgMetadata, so ock rows exist). Filter `rs.term IN
  ["long"]`, order by namespace ASC, limit 1. Baseline: Count=3
  (wrong), page selects key a only → 0 results, HasNext=false (the
  lie — b and c match). Fixed: Count=2, results=[b], HasNext=true,
  and the cursor page returns c.
- RED (namespace): the same shape via `ns.engine IN` or `ns.term IN`
  (keys path requires `ns.stale = false` in params for routing).
- Controls: no detail filters → behavior identical (counts, pages,
  HasNext unchanged); the full existing keyset/pagination suites green.
- Mutations: the EXISTS removed from the count only → the count
  assertion fails; from the page-keys only → the results/HasNext
  assertions fail; the EXISTS identity join dropped (org only) → the
  #840 isolation pattern resurfaces (cross-org eligibility) — assert
  with the two-org fixture if practical. Stash-rerun.

**Non-goals:** #841's reclassification (next cycle); #843/#844 (sort
layer); the unreachable container DISTINCT path (already correct); the
PRE-EXISTING composite multi-value binding bug in ApplyQueryParams
(like_escape(record) on the gpu_model_name OR-composite —
stash-verified present without this change; it is #841-cycle scope
with the already-written composite test as its RED).
