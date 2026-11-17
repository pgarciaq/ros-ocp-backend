# #814 Cycle 1 — ROS Side: Tolerant Reads, Array-Capable Filters, Apply-Normalization — Implementation Plan

> **For agentic workers:** the D1–D5 decisions are recorded on #814
> (2026-11-15, owner-approved). This cycle ships the ROS half; cycle 2
> (koku's builder emitting arrays) follows; cycle 3 optional strictness.
> The forbidden combination never exists within one repo's deploy: ROS
> reads both shapes BEFORE koku emits arrays.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`.
Tracker: #857.

## The surface map (recon 2026-11-18)

| Surface | Location | Today | Cycle 1 |
|---|---|---|---|
| Payload type | `internal/tags/sync.go` `NamespaceTags.Tags map[string]string` | scalar only; an array value fails the WHOLE unmarshal (400) | tolerant: scalar or array → `[]string`; normalization to sorted-unique arrays before the write |
| Sync apply | `sync.go` `SyncOrgTags` — reset + `json.Marshal(tags)` upsert | writes scalars | writes normalized sorted arrays |
| Equality filter (gorm) | `tag_filters.go` `applyAPITagFiltersToKeys`: `@> ?` | scalar containment | OR-of-both-shapes: `@> '{"k":["v"]}' OR @> '{"k":"v"}'` (D2 containment; the transition tolerance) |
| IN filter (gorm) | `tag_filters.go`: `resolved_tags->>? IN ?` | scalar extraction — `->>` on an array returns JSON text (broken) | OR-of-containments per value (D2 any-of, both shapes) |
| Key-exists | everywhere `?` | key existence | **unchanged** — array-agnostic ✓ |
| Raw trios | `tag_filter_keys.go`, `tag_filters.go` `tagFilterAPIExistsClause`, `native_ns_list_keys.go` (onk) | same scalar trio | same both-shapes rewrite |
| Savings summary | `handlers_savings_summary_tag.go`: `resolved_tags->>$n AS tag_value` + GROUP BY | scalar group | `jsonb_array_elements_text(CASE WHEN jsonb_typeof(x)='array' THEN x ELSE jsonb_build_array(x) END)` — one row per value, tolerant |
| Enrichment | `tag_enrichment.go`: unmarshal into `map[string]string` — an array value fails the unmarshal → tags DROPPED | scalar map | tolerant unmarshal (`map[string]json.RawMessage`) + D5 scalar projection (the storage is sorted; first element = alphabetically first) — the response shape unchanged |
| Warnings | `tag_warnings.go` `jsonb_object_keys` | keys only | **unchanged** — array-agnostic ✓ |
| db-mode filters | `tag_filters.go` db sites (since #815) | `= ANY(ls.values)` union | **unchanged** — already the union; api mode now agrees |

## Mechanism

**The tolerance principle (D4):** every read site handles scalar AND
array values; every write site emits sorted-unique arrays. Old rows
(scalar) survive until their org's next push (the sync fully replaces);
new rows are arrays from the first post-deploy push. Koku (cycle 2)
then flips to emitting arrays — ROS is already ready.

**The payload tolerance:** `NamespaceTags.Tags` becomes a custom
`map[string]TagValue` where `TagValue` unmarshals a JSON string OR a
JSON array of strings into `[]string`. The normalization
(sorted-unique) happens before the write — D3's ROS-side defense
(koku sorts in cycle 2; ROS normalizes whatever arrives).

**The filters' both-shapes form:** for value `v` of key `k`:
`(resolved_tags @> jsonb '{"k":["v"]}') OR (resolved_tags @> jsonb '{"k":"v"}')`
— the first matches array storage (D2 containment: `"v" ∈ array`),
the second scalar storage (the transition). Multi-value filters
(a,b) stay any-of: OR over each value's pair. The gorm sites marshal
the two jsonb args; the raw sites add two placeholders per value.

**The savings explosion:** the `->>` extraction becomes a LATERAL
`jsonb_array_elements_text` over the shape-normalized value — a
container with values [a,b] contributes two rows (a and b), each with
its savings; a scalar contributes one. The missing-key behavior is
preserved (NULL group, as today).

**The enrichment projection (D5 phase 1):** unmarshal tolerantly; for
each key, array → first element (the storage is sorted, so first =
alphabetically first — the same rule as today's koku collapse);
scalar → itself. The response shape (`map[string]string`) is
UNCHANGED — koku-ui and IQE untouched.

## Tests

1. **The RED (the payload tolerance):** POST `/internal/tags/sync`
   with a multi-value payload (`"environment": ["production","staging"]`)
   → 202 (baseline: 400 — the array value fails the whole unmarshal).
2. **The filter any-of (api mode):** after applying the multi-value
   payload, `filter[tag:environment]=production` AND
   `=staging` BOTH match the namespace (the #815 db-mode agreement,
   now on the api side). **Baseline: the sync 400s, so nothing matches.**
3. **The scalar-legacy tolerance:** a row with scalar storage
   (`{"environment":"production"}`, seeded directly) still matches the
   filter (the both-shapes form).
4. **The savings explosion:** a namespace with [a,b] contributes both
   tag_value rows.
5. **The enrichment projection:** the response's `tags.environment`
   is the alphabetically-first value; the shape is `map[string]string`.
6. **The sorted-unique normalization:** a payload with unsorted or
   duplicate values writes sorted-unique arrays (verified by reading
   the stored jsonb).
7. **The db≡api agreement (updated #815 test):** both modes match the
   union for the same org data.
8. **Existing tests hold:** the #815 db-mode suite, the tag-warning
   suite, the key-exists tests — unchanged semantics.

## Mutations planned

- **M1** (the tolerance reverted — the unmarshal rejects arrays): the
  RED returns (400).
- **M2** (the scalar-shape arm dropped from the filters): the
  scalar-legacy test fails.
- **M3** (the array arm dropped): the multi-value filter test fails.

## Deliverables

- The sync tolerance + normalization (`internal/tags/sync.go`).
- The filter rewrites (the four api-mode sites).
- The savings explosion (`handlers_savings_summary_tag.go`).
- The enrichment projection (`tag_enrichment.go`).
- The committed benchmark harness (the D1 spike's jsonb side, now
  permanent).
- Tests (the eight above).
- `docs-site/features/tag-filtering.md` — the stored-tags multi-value
  semantics + the phase-1 display projection note.
- CHANGELOG (the pair regenerated — the #800 discipline live); this
  plan; the completion report per properly-implement-feature.

## Implementation record (2026-11-17)

The surface map held; the mechanisms landed with these confirmations and
corrections:

- **Both-shapes helpers, not per-site inlines.** The four api-mode filter
  sites share two helpers — `tagValueContainmentGorm` (gorm `?` args) and
  `tagValueContainmentPgx` (`$n::jsonb` numbered args) in
  `internal/model/tag_filters.go`. The plan's per-site rewrite would have
  duplicated the payload-marshal pair five times; the sites differ only in
  column qualification (`ock.` / `onk.`) and placeholder style.
- **Placeholder numbering correction (caught by the shape test).** In the
  exists-clause builders the orgID arg occupies the first `$n`; containment
  args start at `nextArg+2`, not `nextArg+1` — the first shape assertion
  guessed wrong and the test's own failure showed the real numbering.
- **The savings `->` operator needs `::text`.** A bare `$n` parameter is
  ambiguous between `jsonb->int` and `jsonb->text` (the old code used `->>`,
  which is unambiguous); the LATERAL's three references cast explicitly.
- **The projection sorts defensively.** The plan said "first element
  (storage is sorted)"; the implementation sorts a copy before taking the
  first — legacy rows seeded before the deploy (and test fixtures seeded by
  raw SQL) are not guaranteed sorted, so the projection does not trust
  write discipline. The fixture pins it with an unsorted array.
- **The enrichment RED needed an unfiltered fetch.** Filtering the row via
  a tag filter first would have made the enrichment test fail on the FILTER
  bug at HEAD, masking the unmarshal-drop mechanism; the committed test
  fetches unfiltered and locates the row.
- **`golangci-lint`'s gofmt runs with `-s`** (composite-literal elision:
  `tags.TagValue{"x"}` → `{"x"}` inside a typed map literal) — plain
  `gofmt -w` leaves those; `gofmt -s -w` is what makes lint green.

**Baseline RED (all at HEAD `12bda3aa`, in a temp worktree):** every new
test fails with the mechanism named — the sync 400 ("invalid JSON request
body"), the array rows unmatched ("elements differ"), the savings groups
unexploded, the enrichment tags dropped (nil), the exists-clause still
`->> IN`. **Mutations:** M1 (unmarshal rejects arrays → the 400s return),
M2 (scalar arm dropped → the scalar legacy rows AND the pre-#814 suite
`TestTagFilters_MultiKeyAND_NarrowsResults` lose their matches — the arm is
load-bearing), M3 (array arm dropped → the array rows lose their matches
while the scalar world still passes). All reverted; zero mutation markers
remain; the three suites re-ran green afterwards.

**Benchmarks (the D1 spike's jsonb side, committed):**
`BenchmarkTagFilterKeys_APIMode` (300-ns mixed world: both-shapes
single-value ~0.58ms/query, any-of-two ~0.71ms, the pre-#814 scalar
predicate ~1.78ms over the same rows) and `BenchmarkSyncOrgTags`
(~470ms per 10k-row full-replace sync, mixed shapes). Profile matches the
spike's numbers.

**One observed-once flake, unexplained:** a back-to-back second
`make test-short` reported `FAIL internal/api` once; the immediate rerun
and all targeted full runs (model/api/tags, `-count=1`, non-short) pass.
No failing test name was captured; recorded as environment contention, not
verified.

**Pre-existing uncommitted docs edits (NOT part of this cycle):** the
working tree carried earlier-session edits to `docs-site/features/tag-filtering.md`,
`docs/adr/0353-*.md`, `docs/features/tag-filtering.md`,
`docs/operations/tag-sync-auth.md` describing #815 follow-through AND a
"#867 closed" ADR amendment — #867 is parked, not implemented. The #814
docs hunks were added on top of the same files; the ship gate must decide
how to disentangle (selective staging vs. owner review of the earlier
session's edits) before committing.

## Fresh-context review outcomes (2026-11-17, verified before action)

Verdict: ship-quality core; no P1 introduced by this diff. The reviewer
verified the load-bearing semantics empirically against live Postgres
(both containment arms genuinely necessary — scalar-in-array containment
does NOT apply through object values; the LATERAL's 8-shape NULL/missing-
key matrix; the exact-sum explosion test).

Findings and dispositions:

- **P1 (pre-existing, adjacent): api-mode wildcard 503 on the gorm sites**
  — `ock.resolved_tags ? ?` renders invalid SQL (gorm consumes the jsonb
  `?` operator as a bind marker). Verified empirically (503 on both
  container and namespace lists) and filed as **#872** — not folded into
  this cycle (pre-existing, out of scope; the review's own
  recommendation). The coverage gap (no executed api-mode wildcard test)
  is #872's to close.
- **P2 (rollback hazard): documented.** The tolerance argument is
  forward-only: a rollback to a pre-#814 binary leaves array rows
  unreadable (filters stop matching, tags drop from responses) until each
  org's next push. Added the "Rollback window (#814)" section to
  docs/operations/tag-sync.md.
- **P2 (pgx array-arm unexecuted): closed with three executed tests** —
  node-utilization (ExistsClause; discriminated by the array-only
  tier=gold, since the base fixture's scalar staging would mask it),
  cluster-quota (CommaNamespaces), and MatchingContainerKeys (model
  level). Mutation-verified: all three fail under a re-run of M3.
- **P3 (savings trusts uniqueness, enrichment doesn't): documented as a
  trust boundary** in the code comment; the dedup (DISTINCT lateral)
  deferred — duplicates are unreachable through the sync (sorted-unique
  normalization), so no demonstrated need.
- **P3 (catalog values unnormalized): fixed** — normalizeTagKeys now
  applies the same D3 defense (sorted-unique, empty dropped, blank keys
  dropped); the normalization test's catalog half pins it (fails on the
  old raw-store behavior).
- **P3 (TagValue doc comment): corrected** — a JSON null element decodes
  as "" (Go's null-into-string no-op), benign because normalization drops
  empties; only other non-string elements are rejected.
- **P3 (recorded, no action — unreachable via the sync):** empty array
  `{"k":[]}` vanishes the container from savings groups (old `->>` made a
  `"[]"` text group); enrichment treats scalar null and `[null]`
  asymmetrically (skipped vs `""`); namespace-tag values have no per-key
  cap (bounded only by the 10MB body limit — the catalog path caps at
  500). The per-key cap is a cycle-2 coordination question for the owner,
  not silently added here.
- **P3 nit:** the stray blank line in applyDBTagFiltersToNSKeys removed.
