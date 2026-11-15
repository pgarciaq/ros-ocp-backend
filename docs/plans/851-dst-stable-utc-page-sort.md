# #851 — Stable UTC Text for the Page-Sort Cast — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner go-ahead 2026-11-15, following the readiness discussion: mechanism =
> **stable UTC text** (owner-approved); the NS DISTINCT fallback stays
> **untouched** (typed ordering — the reference implementation; the bug is not
> there and its anchor round-trip depends on the offset-carrying render).

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`.
Tracker: #857 (serving P3 blitz, item 3).

## The verified mechanism (the readiness recon)

- The page-sort value is cast `(%s)::text` and both the page-subquery
  ORDER BY and (part of) the cursor seek operate on that TEXT render.
  `timestamptz::text` renders in the **session timezone** with a
  variable UTC offset, so text ordering can disagree with chronological
  ordering across DST boundaries. Concrete reversal (the fall-back
  ambiguity, America/New_York, 2026-11-01): row A `05:30Z` renders
  `01:30:00-04` (first pass), row B `06:15Z` renders `01:15:00-05`
  (second pass) — chronologically A < B, textually B < A.
- The exposed sort is exactly `last_reported` (container:
  `rs.updated_at`; namespace: `c.last_reported_at`). Every other sort is
  numeric (handled by the numeric-cast path) or plain text.
- **The cast sites that order by the text render** (where the bug lives):
  1. NS keys path — `native_ns_list_keys.go:350` (reached when the query
     carries `ns.stale = false`)
  2. Container org-keys path — `recommendation_set_native.go:429`
     (reached with tag filters)
  3. Container DISTINCT path — `recommendation_set_native.go:597`
     (reached with plain params)
- **The NS DISTINCT fallback** (`namespace_recommendation_set_native.go:195`)
  orders by the TYPED `ros_ns_page_sort_raw` — correct; its text column is
  cursor-anchor-only and round-trips because the session render carries the
  offset. **Untouched.**
- Column types verified: `clusters.last_reported_at` and
  `recommendation_sets.updated_at` are `TIMESTAMPTZ` — so
  `expr AT TIME ZONE 'UTC'` yields a naive UTC timestamp (the right
  direction for the render below).

## The design hazard the recon found — the anchor round-trip

The cursor anchor round-trips: DB text render → `nativeNSParseSortText`
(tries RFC3339 — **always fails today**, PG renders with a space
separator) → verbatim string → cursor JSON (`PaginationSortValueJSON`) →
`DecodePaginationSortValue` (string branch, RFC3339 fails again) → the
same string bound into the seeks.

**Consequence 1 — the format must NOT be RFC3339-parseable.** A naive
"UTC ISO 8601" render (`T` + `Z`) would parse in both functions,
silently converting the anchor to a `time.Time`, which then binds
against the text-seek columns with unpredictable pgx text encoding —
and against typed exprs in yet other places. The chosen format is
deliberately unparseable.

**Consequence 2 — the typed seeks need a matching wrap.** The keys and
container paths apply TWO seek layers: an inner **typed** seek on the
raw timestamp expr (`applyNativeNSKeysPageSeek` on `c.last_reported_at`;
`applyNativeContainerPageSeek` on `rs.updated_at`) and an outer **text**
seek on the page-sort column. Today's anchor carries the session offset,
so PG parses it correctly for the typed seek. An offset-less UTC anchor
would be parsed **in the session timezone** (5h off under NY) — the
inner seek would mis-seek. Fix: for timestamp sorts, wrap the typed
seek's expr as `(expr) AT TIME ZONE 'UTC'` so both sides are naive UTC.

## Mechanism (the fix)

1. `isTimestampPageSort(sortExpr)` — contains `updated_at` or
   `last_reported_at` (style-matched to the existing
   `isNumericNSSort`/contains checks).
2. `pageSortTextExpr(sortExpr)` — timestamp sorts render as
   `to_char((expr) AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS.US')`;
   everything else keeps `(expr)::text`. Applied at the three cast
   sites. Properties: fixed-width (`.US` always 6 digits), so
   lexicographic order == chronological order; **DateStyle-independent**
   (unlike `::text`, which also breaks under non-ISO DateStyle — a
   robustness win beyond DST); NULL-preserving for the NULLS
   FIRST/LAST handling; space separator + no offset → not
   RFC3339-parseable → the anchor stays a verbatim string.
3. UTC-wrap the inner typed seeks for timestamp sorts:
   `applyNativeNSKeysPageSeek` and `applyNativeContainerPageSeek`
   compare `(expr) AT TIME ZONE 'UTC'` (naive UTC) against the
   offset-less anchor string (param inferred as `timestamp`, parsed
   naive — no session-tz shift). NOT `applyNativeContainerKeysPageSeek`
   (keys-only sorts — no timestamp exprs reachable). NOT the NS
   fallback (untouched).
4. `nativeNSParseSortText` **unchanged** — the new format keeps failing
   its RFC3339 parse, preserving the verbatim-string anchor on every
   path. A comment documents that the format is deliberately
   non-RFC3339 (changing it requires re-auditing every seek bind site).

## Tests (final)

All against the shared testcontainer DB; a second pgx pool built from the
shared pool's config with `RuntimeParams["timezone"] =
"America/New_York"` gives the DST-observing session (`database.DB` set to
a gorm DB built **directly** on that pool — `testutil.OpenTestGORM` is a
`sync.Once` keyed on the first pool and would silently return the shared
UTC gorm; found the hard way during the RED). Fixture instants: A =
`2026-11-01 05:30:00+00` (renders `01:30:00-04`), B =
`2026-11-01 06:15:00+00` (renders `01:15:00-05`) — chronological A < B,
textual B < A.

1. **NS keys path** (`ns.stale = false` → the keys path; two clusters,
   one namespace each, direct `NamespaceRec` seeding — the established
   keys-path fixture pattern): ASC/DESC by `last_reported` chronological
   + cursor-continued page 2. **RED on baseline — two symptoms:** page 1
   mis-ordered AND page 2 empty (the typed seek cannot match the
   mis-ordered anchor).
2. **Container org-keys path** (the always-live container route —
   `usesOrgContainerKeys` returns true unconditionally): two containers
   with `rs.updated_at` = A/B, same ASC/DESC + continuation assertions.
   **RED on baseline — the same two symptoms.**
3. **NS DISTINCT fallback control** (plain params → the typed path):
   correct on baseline AND after the fix — pins that the reference path
   stays untouched.
4. **Sub-second ordering guard**: two containers one microsecond apart
   inside the same second, identity order deliberately reversed — pins
   the `.US` fixed-width render (a second-truncating render falls
   through to the identity tie-break and reorders). Passes on baseline
   under a UTC session (sub-second `::text` renders are chronological)
   — a regression guard for the fix's format.

**Reachability finding (caller check):** the container DISTINCT variant
(`getNativeRecommendationsDistinct`, the third cast site) is
**production-dead** — `usesOrgContainerKeys` always returns true, so the
list always routes through the org-keys path; only an internal test
calls the DISTINCT variant directly. Its cast is fixed for consistency
(no dedicated E2E RED); noted in ADR-0357.

## Mutations run

- **M1** (revert the render to session-tz `::text`): the RED itself —
  page 1 mis-ordered, page 2 empty, on both keys paths.
- **M2** (drop the typed-seek UTC wrap, keep the render): the
  offset-less anchor is parsed in the NY session for the typed seek →
  page 2 empty on BOTH continuation tests; the control and the
  sub-second guard correctly still pass (no cursor / no DST dependency).
- **M3** (second-truncating format, no `.US`): the sub-second guard
  fails — the tie falls through to the identity tie-break and reorders.

## Scope notes

- The NS DISTINCT fallback: untouched (owner decision — typed ordering,
  correct; its anchor depends on the offset-carrying render).
- Version skew: cursors issued before a deploy (session-tz text)
  compared against the new UTC render can skip/duplicate rows for one
  in-flight page chain; self-corrects on the next cursor. CHANGELOG
  deploy note.
- IQE/UI: the cursor is opaque; UTC-session test runs see no order
  change. No coordination needed.
- No docs/ or docs-site/ pagination page mentions the page-sort text
  format (verified by grep) — the ADR is the record.

## Deliverables

- Fix + tests (`namespace_recommendation_pagination.go` — the three
  helpers; `native_ns_list_keys.go` and
  `recommendation_set_native.go` — the cast sites and the ns keys seek;
  `container_recommendation_pagination.go` — the container seek;
  `page_sort_dst_test.go` — the four tests).
- ADR-0357 (new) + both ADR indexes (internal README, docs-site page).
- CHANGELOG entry (user-visible fix + the version-skew deploy note);
  this plan doc; completion report per properly-implement-feature.

## Fresh-context review (no P1/P2; four P3s — all handled)

- **P3 cross-path NS cursor replay** (fallback cursor deliberately
  replayed on the keys route, or vice versa — post-fix the formats
  differ): mitigated by served-links never crossing routes
  (`keysetNextURL` copies the request filters); documented in
  ADR-0357's consequences with the self-correcting remediation.
- **P3 two substring vocabularies** (`isTimestampPageSort` vs
  `nativeNSParseSortText`'s `last_reported` branch): forward-looking
  fragility; the maintenance requirement is now documented in
  ADR-0357's consequences.
- **P3 dead RFC3339 branch** in `nativeNSParseSortText`: unreachable
  for every DB-sourced render (verified empirically by the reviewer
  with a Go probe — including 5-digit-year and BC strings); harmless
  belt-and-braces, left in place, documented here.
- **P3 4-digit-year bound + seek index eligibility**: both documented
  in ADR-0357's consequences; the index impact is bounded (residual
  predicate on org-narrowed joins).

The reviewer independently verified: the RED mechanism (stashed the
fix, re-ran — both keys-path tests fail at the page-2 fetch, the exact
#851 mechanism, not an incidental fixture failure); the anchor
round-trip end-to-end (no render parses as RFC3339); seek/order
agreement at all three cast sites including NULL polarity and
tie-breakers; every unwrapped seek's reachability; the allowlist walk
(no false positives/negatives in `isTimestampPageSort`); injection
surface (sortExpr always allowlist-derived); cursor opacity
(`PaginationSort` is `json:"-"`); full model + api suites; docs gates.
