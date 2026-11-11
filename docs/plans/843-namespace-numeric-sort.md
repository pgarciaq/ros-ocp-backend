# #843 Namespace Numeric Sort — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner go-ahead 2026-11-11 (the queue: #843 after #841, per
> docs/plans/841-844-list-architecture-queue.md).

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `c6a1262d`. Tracked as #843 (branch-review finding 4, tracker #849).

**Recon facts (verified in code):**
- The namespace keys path casts the sort expression to TEXT:
  `(%s)::text AS ros_ns_page_sort` (native_ns_list_keys.go:~318 and the
  page-subquery Select). The outer ordering sorts that text.
- Executed PostgreSQL probe (Sol): descending savings values returned
  `2, 100, 10` instead of `100, 10, 2`.
- The namespace DISTINCT fallback preserves a TYPED raw value
  (`namespace_recommendation_set_native.go:187–207` — a
  `ros_ns_page_sort_raw` variant); the keys path does not.
- The container path handles this correctly with numeric casts:
  `container_recommendation_pagination.go:50–80` — `isNumericPageSort`
  (suffix detection: `_cents`, `_millicores`, `_kib`, `_bytes`,
  `_days`, `_pct`) and `pageSortCol` (`(col)::bigint` when numeric).
- The sort expression flows through three layers in the keys path:
  (1) the keys-page ORDER BY (`nativeNSKeysPageOrder` or
  `nativeNSKeysDistinctOnOrder`), (2) the page-subquery Select +
  ORDER BY (`nativeNSKeysSubqueryPageOrder`), (3) the detail ORDER BY
  (`nativeNSKeysDetailOrder`) and the cursor seek
  (`applyNativeNSPageSeekOnPageKeys`). All three must agree on the
  typed ordering.
- The cursor anchor (`NamespacePaginationAnchor.SortValue`) carries
  the sort value — its type must match what the seek compares
  against.

**Mechanism:** mirror the container's approach on the namespace keys
path:
1. `isNumericNSSort(sortExpr)` — suffix detection on the REMAPPED
   namespace sort expressions (the NRS column names:
   `estimated_savings_cents`, `estimated_waste_cents`,
   `idle_duration_days`, `cpu_variation_*_pct`, etc.).
2. The page-subquery Select carries BOTH values (like the DISTINCT
   fallback): `(%s) AS ros_ns_page_sort_raw` (typed) and
   `(%s)::text AS ros_ns_page_sort` (for the anchor/serialization if
   needed — or replace the text cast entirely with the raw value).
3. The three ORDER BY sites and the cursor seek use the typed value
   with a numeric cast when `isNumericNSSort` is true (mirroring the
   container's `pageSortCol`).

**Tests:**
- RED: seed namespaces with savings values of different digit lengths
  (e.g., 2, 10, 100 cents); sort by estimated_monthly_savings DESC;
  assert the order is `100, 10, 2` — baseline returns `2, 100, 10`.
- Controls: sort by namespace_name (text) unchanged; sort ASC and
  DESC; the cursor seek across pages maintains the typed ordering;
  NULLs sort last.
- Mutations: the numeric cast removed → the RED returns; the cast
  applied to text sorts → the namespace_name control fails; the
  suffix detection inverted → the numeric control fails.

**Non-goals:** #844 (alias translation — separate cycle); the
container path (already correct); the DISTINCT fallback (already
typed).
