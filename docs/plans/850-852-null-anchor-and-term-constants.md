# #850 + #852 — NULL-Anchor Ascending Seek + Container Term Constants — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner go-ahead 2026-11-12: both issues in one cycle (small, pre-existing,
> surfaced by the review cycles, same serving layer).

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `1a7f83b6`. Tracked as #850 and #852 (pre-existing, not under
tracker #849).

## #850 — NULL-anchor ascending seek

**Recon (verified by reading the seek code):**
- The ASC seek predicate: `((sortCol) > ? OR (sortCol IS NOT DISTINCT
  FROM ? AND tie > (?, ?)))`. When the anchor value is NULL:
  - `sortCol > NULL` → NULL (never true)
  - `sortCol IS NOT DISTINCT FROM NULL` → true only for NULL rows
- So after exhausting the NULL block (by tiebreaker), the seek can't
  reach the non-NULL rows that follow (NULLS FIRST in ASC).
- DESC is already correct: NULLS LAST means a NULL anchor is at the
  end — nothing follows.

**Mechanism:** when `sortValue == nil` (the anchor's sort value is
NULL), the ASC seek becomes:
`(sortCol IS NULL AND tie > anchor_tie) OR (sortCol IS NOT NULL)`
— continue within NULLs by tiebreaker, then jump to the non-NULL rows.

**Sites:** both seek functions — the container path
(`internal/model/recommendation_set_native.go` and/or
`container_recommendation_pagination.go`) and the namespace path
(`internal/model/native_ns_list_keys.go`).

**Tests:**
- RED: a fixture where a page boundary falls on a NULL sort value in
  ASC order, with non-NULL rows following — the cursor continuation
  must reach them (baseline: the traversal stops).
- Controls: DESC with NULLs (already correct, unchanged); ASC without
  NULLs (unchanged); the #843 numeric-sort cursor walk (green).
- Mutations: the NULL-anchor branch removed → the RED returns; the
  NULL-anchor branch applied to DESC → the DESC control fails.

## #852 — Container term constants

**Recon (verified):**
- `containerVariationTermSuffix` (recommendation_set_native.go:31–35)
  maps `"short"` → `kruizeplugin.ShortTerm = "short_term"` — but the
  engine emits `"short"` (librobne/engine/defaults.go:35–37).
- All 12 container variation sorts filter for `"short_term"` /
  `"medium_term"` / `"long_term"` — zero matching rows in production.
- The namespace side's fix (#844) already replaced the kruizeplugin
  constants with the DB values.

**Mechanism:** replace the map values with `"short"`, `"medium"`,
`"long"` — identical to the namespace-side fix. Remove the
kruizeplugin import if no longer used.

**Tests:**
- RED: sort by `cpu_variation_short_cost` on the container list with
  production term values (`"short"`) — baseline returns empty (the
  filter matches zero rows).
- Controls: the #844 variation tests (namespace side) stay green.

**Non-goals:** #845 (sleep timezone); #846–#848 (P3s); the EXISTS
verification (alongside or after).
