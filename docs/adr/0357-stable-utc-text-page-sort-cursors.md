# ADR-0357: Stable UTC text for page-sort cursor anchors and ordering

## Status

Accepted (implements #851)

## Context

The container and namespace list endpoints paginate through a page subquery
whose sort value is selected as `(sort_expr)::text AS ros_*_page_sort`. Both
the page-subquery ORDER BY and part of the cursor keyset seek operate on that
text render, and the cursor anchor round-trips the same text through JSON.

`timestamptz::text` renders in the **session timezone** with a variable UTC
offset. Across a DST boundary the render order disagrees with chronological
order — concretely, in America/New_York on the fall-back date the local hour
01:00–01:59 occurs twice; an instant from the first pass renders `…01:30:00-04`
and one from the second pass `…01:15:00-05`, so text ordering serves the later
instant first. Worse, the keyset seek compares the typed column against the
anchor, so the mis-ordered page 1 produces an anchor the continuation cannot
match: page 2 comes back empty (rows lost, not just reordered).

The exposed sorts are exactly `last_reported` (`recommendation_sets.updated_at`
on the container path, `clusters.last_reported_at` on the namespace path);
every other sort is numeric (already handled by a numeric cast) or plain text.

## Decision

Render timestamp page-sort values as **stable UTC text**:

```sql
to_char((sort_expr) AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS.US')
```

and adapt the typed keyset seeks to compare `(sort_expr) AT TIME ZONE 'UTC'`
(naive UTC) so the offset-less anchor is not parsed in the session timezone.

Properties that constrain the format:

- **Fixed width** — `.US` always renders six digits, so lexicographic order
  equals chronological order even for sub-second values (a second-truncating
  render falls through to the identity tie-break and reorders).
- **DateStyle-independent** — `to_char` does not depend on the session
  `DateStyle`, unlike `::text` (which also breaks under non-ISO settings).
- **Deliberately NOT RFC3339-parseable** (space separator, no offset). The
  anchor round-trip (`nativeNSParseSortText` → cursor JSON →
  `DecodePaginationSortValue`) must keep the value a verbatim string: a
  parseable format silently converts it to `time.Time`, which then binds
  against the text-seek columns with unpredictable encoding. Changing the
  format requires re-auditing every seek bind site and both parse functions.

## Alternatives Considered

### Typed sort value through the page selection (the NS DISTINCT fallback's `ros_ns_page_sort_raw`)

Carries the typed value alongside the text and orders/seeks on the typed
column. Correct, but requires threading a second column through every page
subquery and scan struct on both list paths; the fallback keeps it only
because its anchor text also feeds a typed seek that depends on the
offset-carrying session render.

### UTC ISO 8601 with `T`/`Z` (RFC3339)

Parseable by both parse functions — silently changes the anchor's Go type and
breaks the text-column seeks (see above). Rejected on the round-trip hazard,
not on rendering quality.

## Consequences

- Page ordering by `last_reported` is chronological in every session
  timezone, and cursor continuation agrees with page 1.
- The NS DISTINCT fallback (typed ordering) is intentionally untouched: it
  was already correct, and its anchor round-trip depends on the
  offset-carrying render.
- **Cross-path cursor replay:** an NS cursor taken from the fallback route
  (no `filter[stale]`) and deliberately replayed on the keys route
  (`filter[stale]=false`), or vice versa, now shifts the seek by hours —
  the two routes' anchors carry different formats (offset-carrying vs
  offset-less UTC). Served `links.next` URLs copy the request's filters, so
  a client following links never crosses routes; only manual query-string
  reconstruction can. Same remediation as the version skew below: the next
  cursor self-corrects.
- **Adding a timestamp sort** to `ContainerAllowedOrderBy` /
  `NsAllowedOrderBy` requires extending BOTH `isTimestampPageSort` and the
  `last_reported` branch of `nativeNSParseSortText` — the two substring
  vocabularies are not enforced against each other by any test.
- The fixed-width ordering guarantee holds for 4-digit years (`to_char`
  renders 5 digits from year 10000); the columns are `now()`-stamped, so
  this is not a practical bound.
- The wrapped seek (`expr AT TIME ZONE 'UTC' > $1`) cannot use a plain
  btree index on the bare timestamp column. Impact is bounded: on both
  live paths the seek is a residual predicate on rows already narrowed by
  the org-scoped keys joins.
- The container DISTINCT variant (`getNativeRecommendationsDistinct`) gets
  the same render for consistency although it is currently unreachable in
  production (`usesOrgContainerKeys` always returns true; only an internal
  test calls it directly).
- **Version skew:** cursors issued before this change carry session-tz text;
  compared against the new UTC render they can skip or duplicate rows for
  one in-flight page chain. Self-corrects on the next cursor. Noted in the
  CHANGELOG.
- Non-timestamp sorts are unchanged (`(expr)::text`).

## References

- [internal/model/namespace_recommendation_pagination.go](../../internal/model/namespace_recommendation_pagination.go) — `pageSortTextExpr`, `timestampSeekExpr`
- [internal/model/native_ns_list_keys.go](../../internal/model/native_ns_list_keys.go)
- [internal/model/recommendation_set_native.go](../../internal/model/recommendation_set_native.go)
- [internal/model/container_recommendation_pagination.go](../../internal/model/container_recommendation_pagination.go)
- #851, #843 (the fresh-context review whose PostgreSQL probe verified the reorder)
