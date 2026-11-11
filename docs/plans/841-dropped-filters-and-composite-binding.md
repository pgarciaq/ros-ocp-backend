# #841 Dropped Filters + Composite Binding — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner go-ahead 2026-11-11 (the queue: #842 → #841 → #843 → #844). This
> cycle's scope: the filed finding PLUS the composite-binding fix inherited
> from #842's review (documented in
> docs/plans/841-844-list-architecture-queue.md).

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `e3b440d6`. Tracked as #841 (branch-review finding 2, tracker #849).

**Scope A — the filed finding (dropped filters):**
- `rs.engine IN ?` and `rs.category IN ?` (container) and
  `ns.category IN ?` (namespace) are ACCEPTED by the allowlists
  (`native_query_allowlist.go:16–18,74–77`; emitted at
  `handlers.go:405–445`, `utils.go:849–860`) but appear in NEITHER
  classification table → they land in the keys leg, the remapper logs
  "skipping unknown query key", and the filter is silently dropped.
- They are recommendation-dependent predicates: the correct
  classification is detail-only (`nativeRecDetailOnlyQueryKeys` /
  `nativeNSDetailOnlyQueryKeys`), where #842's eligibility machinery
  (now landed) makes them gate counts, page selection, and rows
  consistently.
- Note: `ns.engine IN ?` is ALREADY detail-only on the namespace side;
  only the container `rs.engine` and both `category` keys are dropped.

**Scope B — the inherited composite-binding fix (pre-existing):**
- `ApplyQueryParams` passes a flat value slice to keys with multiple
  placeholders — the gpu_model_name OR-composite (handlers.go:370–381)
  errors with `like_escape(record)` in the DETAIL query today
  (stash-verified during #842's cycle).
- The fix: distribute positional values across a key's placeholders
  when the key is not a single `IN ?` (GORM expands single-slice args
  for `IN ?` but binds a multi-placeholder key's flat slice as one
  record). The binding rule mirrors what handlers produce: composite
  keys carry one flat slice, positional across their `?`s.
- The eligibility helpers (#842) use the same per-key Where binding —
  fixing ApplyQueryParams' binding fixes both paths if the helpers
  route through the same distribution. SURVEY first: the eligibility
  helpers call `query.Where(prefix+" AND ("+key+"))", values)` —
  the same shape — so the distribution fix must live in a shared
  helper both call.
- The RED: the composite test written during #842
  (TestGetNativeRecommendations_CompositeDetailFilterEligibility —
  removed from the tree because a failing test cannot ship; restore
  it as this cycle's RED).

**Tests:**
- Scope A RED: request `filter[category]=zombie` (or engine) on both
  lists with a fixture where some keys carry the category and others
  don't — baseline: the filter is dropped (all keys returned, count
  unfiltered); fixed: only matching keys, counts filtered, eligibility
  consistent. Assert through the model layer with the classification
  the API produces (the exact queryParams keys the handlers emit).
- Scope A controls: the #842 eligibility tests stay green (term
  filters); unfiltered lists unchanged.
- Scope B RED: the restored composite test (like_escape error on
  baseline) + a POSITIVE composite control (values matching a seeded
  gpu_model → rows returned, not just the empty-match case).
- Mutations: classification removed (a category key back to the keys
  leg → the drop returns); binding distribution removed (the composite
  errors again); distribution applied to IN keys (breaks IN expansion
  → the term-filter tests fail). Stash-rerun both REDs.

**Mechanism summary:**
1. Add `rs.engine IN ?`, `rs.category IN ?` to
   `nativeRecDetailOnlyQueryKeys`; add `ns.category IN ?` to
   `nativeNSDetailOnlyQueryKeys`.
2. A shared `bindQueryParams(query, params)` (or a values-distribution
   wrapper) used by ApplyQueryParams AND the two eligibility helpers:
   for each key, count top-level placeholders; if the key contains
   `IN ?` → pass the slice as one arg (GORM expands); else if the
   value is a slice → distribute positionally across the placeholders;
   else → single scalar.
3. The detail-only classification for category/engine needs the
   columns to EXIST on the recommendation tables (rs.category ✓ —
   in the writer's INSERT; ns category — VERIFY the namespace table
   has a category column; if not, scope A's namespace half is the
   finding's exact text re-check: the review said `ns.category IN ?`
   is accepted — verify the column exists before classifying).

**Non-goals:** #843/#844 (sort layer); #845+; the OpenAPI spec
categories (the filters are already documented — their behavior is
what's broken).
