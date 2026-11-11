# #841–#844 Joint Recon — The Native List Architecture Queue

> **For agentic workers:** superpowers:test-driven-development governs every
> step of every cycle. This document is the shared recon the four cycles
> build on; each issue keeps its own plan, ledger, and report.

**Owner go-ahead 2026-11-11** ("do it"): the four list-architecture P2s
from the branch review (#841–#844), cycled in the composition order below.

## The shared machinery (verified by reading)

Both native lists (container, namespace) split into a KEYS leg and a
DETAIL leg:

1. `splitNativeListQueryParams` / `splitNativeNSListQueryParams`
   classify every accepted predicate: detail-only sets
   (`nativeRecDetailOnlyQueryKeys`, `nativeNSDetailOnlyQueryKeys`) vs
   the keys leg (allowlisted atoms remapped to the keys table via
   `nativeRecKeysFilterAtoms` / `nativeNSKeysFilterAtoms`).
2. COUNT and PAGE-KEYS queries run on the keys table (org-scoped,
   RBAC, tag filters) — with ONLY keys-leg predicates applied.
3. Page keys are selected + limited, then the DETAIL query joins the
   recommendation table by identity and applies detail-only predicates
   — AFTER pagination. `HasNext` is computed from surviving results;
   counts never saw the detail predicates.
4. Sorting: the page subquery casts the sort expression to TEXT
   (`(%s)::text AS ros_ns_page_sort`); the outer ordering sorts that
   text. The container path numeric-casts
   (`container_recommendation_pagination.go:50–80`,
   `isNumericPageSort` suffix detection: `_cents`, `_millicores`,
   `_kib`, `_bytes`, `_days`, `_pct`); the namespace keys path does
   not.

## How the four findings compose

**Layer 1 — predicate classification & eligibility (#842 then #841):**
- #842: the detail-only predicates that DO work (container `rs.term`,
  `rs.idle_state`, …; namespace `ns.term`, `ns.engine`, `ns.idle_state`)
  lie — counts omit them and `HasNext` truncates traversal when a page
  of nonmatches precedes matches. Fix: apply recommendation
  eligibility to count and page-key selection (an appropriately scoped
  `EXISTS` against the recommendation table) — Sol's executed probe:
  `reported_count=3 | returned_rows=0`, `HasNext=false`.
- #841: `rs.engine IN ?`, `rs.category IN ?` (container) and
  `ns.category IN ?` (namespace) are ACCEPTED by the allowlists
  (`native_query_allowlist.go:16–18,74–77`; emitted by
  `handlers.go:405–445`, `utils.go:849–860`) but appear in NEITHER
  classification table → they land in the keys leg, the remapper logs
  "skipping unknown query key", and they are silently dropped. They
  are recommendation-dependent predicates: the correct classification
  is detail-only — where #842's eligibility machinery then makes them
  work correctly.
- **Order: #842 FIRST** (eligibility infrastructure for the existing
  detail-only keys), then #841 (reclassify the dropped keys into the
  now-safe detail-only set). The reverse order would transiently ship
  working-but-lying filters.
- **#841-cycle scope addition (found during #842's review):** the
  PRE-EXISTING composite multi-value binding bug — ApplyQueryParams
  passes a flat value slice to keys with multiple placeholders, so the
  gpu_model_name OR-composite (handlers.go:370–381) errors with
  like_escape(record) in the DETAIL query today (stash-verified on
  baseline during #842). The composite eligibility test written during
  #842 is that fix's RED; fix the binding (per-placeholder value
  distribution) in ApplyQueryParams so both the detail and eligibility
  paths bind composites correctly.

**Layer 2 — namespace sort (#843 then #844):**
- #843: the text cast makes numeric sorts lexicographic on the keys
  path (executed probe: `2, 100, 10` descending). The namespace
  DISTINCT fallback already preserves a typed value
  (`namespace_recommendation_set_native.go:187–207`); the keys path
  does not. Fix: carry a typed sort value through selection, ordering,
  and cursor seeking — mirroring the container's `pageSortCol`
  numeric-cast approach.
- #844: the legacy sort aliases (`cpu_request_current`,
  `memory_request_current`, `cpu_variation_{term}_{engine}_pct`, from
  `list_options.go:131–146`) target columns the NATIVE writer never
  populates — it writes `current_cpu_request_millicores`,
  `current_memory_request_kib`, and ROW-LEVEL
  `variation_cpu_request_pct` etc. (one row per term/engine;
  `librobne/pgrec/namespace.go:31–59`). The container path translates
  aliases and selects the relevant term/engine
  (`recommendation_set_native.go:54–76`); namespace lacks that
  translation. Fix: namespace-specific native sort translation,
  including term/engine row selection for variation aliases.
- **Order: #843 FIRST** (typed ordering for the existing numeric
  columns — `estimated_savings_cents` etc.), then #844 (alias
  translation; its row-level variation expressions then flow through
  the typed machinery).

## Sequence

**#842 → #841 → #843 → #844.** Each is a full cycle: baseline RED,
mutation checks, adversarial fixtures, fresh-context review, completion
report, owner go-ahead at close-out.

## Shared test infrastructure (noted for reuse)

- The container harness: `setupNativeListGormDB` +
  `seedNativeListCluster` + `testutil.SeedDigestSeriesFrom` +
  `engine.RecommendAllWorkloads` + `WriteRecommendationsAndRefreshOrg`
  (real pipeline; `recommendation_set_keyset_test.go`).
- The namespace harness: `seedNativeNamespacePagination`
  (`namespace_recommendation_keyset_test.go`) and the compat seeder.
- #840's two-org fixture pattern for any isolation-adjacent assertions.
