# #844 Namespace Sort Alias Translation — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner go-ahead 2026-11-12, mapping table approved (same term constants as
> the container: kruizeplugin.ShortTerm/MediumTerm/LongTerm).

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `a7292b5f`. Tracked as #844 (branch-review finding 5, tracker #849).

**Recon facts (verified in code):**
- The namespace sort path (`nativeNSPageSortExpr` →
  `remapNativeNSOrderBy`) only swaps the table prefix — legacy column
  names pass through, targeting columns the native writer never
  populates (`cpu_request_current` vs `current_cpu_request_millicores`).
- The container path is the reference: `nativeContainerSortExpr`
  (recommendation_set_native.go:54–76) returns (sortExpr, rsFilter) —
  variation aliases get a regex-parsed term/engine, the rsFilter
  becomes a WHERE on the joined rs in the keys page
  (recommendation_set_native.go:411–413).
- The namespace keys path already joins namespace_recommendation_sets
  via `nativeNSKeysNRSJoin` for non-keys-only sorts.
- `_current` suffix in `isNumericNSSort` (#843's stopgap) becomes dead
  after this translation — removed per owner decision.

**Mechanism:**
1. `namespace_recommendation_pagination.go`:
   `nativeNSSortExpr(orderByDBCol) → (sortExpr, nsFilter)` —
   regex-parses variation aliases (the container's pattern adapted),
   maps `cpu_request_current` / `memory_request_current` to native
   columns, passes everything else through the existing prefix swap.
2. `native_ns_list_keys.go`: the `nsFilter` flows as a WHERE on the
   `nrs` join in the keys-page query (same as the container's
   `sortFilter`).
3. `_current` removed from `isNumericNSSort`.

**Tests:**
- RED: sort by `cpu_request_current` with seeded native-column values
  (2/10/100 millicores — text and numeric order differ, proving both
  the alias translation AND the numeric cast). Baseline: the sort
  targets the unpopulated legacy column (all NULLs → no ordering).
- RED: sort by `cpu_variation_short_cost` with per-term/engine
  variation values. Baseline: targets the legacy per-term column.
- Controls: savings sort (already correct) unchanged; namespace_name
  (keys-only) unchanged; the #843 numeric-sort and cursor-walk tests
  green.
- Mutations: translation removed (legacy column returns → all-NULL
  ordering); term/engine filter dropped (wrong row's variation); the
  `_current` suffix check removed (no effect post-translation — the
  native columns have their own suffixes).

**Non-goals:** #850 (NULL-anchor, next cycle); the EXISTS
verification (alongside); #851 (DST); the container path (already
correct).
