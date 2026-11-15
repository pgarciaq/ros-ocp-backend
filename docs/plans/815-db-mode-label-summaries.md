# #815 — db-Mode Tag Filtering Reads the Label Summaries — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner decisions 2026-11-15: **option B** — repoint ROS db mode at Koku's label
> summaries (ADR-0353's source + window), making db≡api by construction; the
> **disable fix folded in** (query-time `EXISTS` on `reporting_enabledtagkeys`);
> the koku-side arrays overwrite **filed as a koku issue** (durable correctness
> for any future consumer); a **measured synthetic benchmark** in the report.
> Context from the owner: Cost Management caps **200 enabled tags** — the label
> summaries contain enabled keys only, so the predicate's inner side is bounded
> by 200 keys × namespaces × nodes × window periods.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`.
Tracker: #857.

## The verified mechanism (the issue's executed reproduction)

`reporting_ocptags_values` arrays are rebuilt per summarization run from that
run's report periods only (the `SET`, not a merge, since 2022) — for a
key/value shared by two clusters, the last-summarized cluster owns them. ROS's
db-mode predicates unnest those arrays, so a tag filter silently drops every
cluster except the last one summarized. Both the pod and the storage label
SQL write the arrays — no divergence between the two sources there.

## Mechanism (the fix)

**Source:** the two label-summary tables the Tags API and ADR-0353's push
read — `reporting_ocpusagepodlabel_summary` and
`reporting_ocpstoragevolumelabel_summary` (identical shape: `key`,
`values[]`, `report_period` FK, `namespace`, `node`; composite unique index
on `(key, report_period, namespace, node)`), joined through
`reporting_ocpusagereportperiod` (unique index leading on `cluster_id`) for
the cluster.

**Window:** ADR-0353's month-snap, mirrored exactly — report periods with
`report_period_start >= last_month_start AND < next_month_start` (equivalent
to the push's `<= this_month_end` for month-aligned period starts). A cluster
keeps matching while one of its periods is in the window — the same
staleness profile as api mode (agreement, not a new window decision; the
value-side retention question stays where the owner filed it).

**Predicate shape** (per filter, correlated on the caller's cluster/namespace
expressions):

```sql
(EXISTS (
    SELECT 1
    FROM <schema>.reporting_ocpusagepodlabel_summary ls
    JOIN <schema>.reporting_ocpusagereportperiod rp ON rp.id = ls.report_period_id
    WHERE ls.key = ?
      AND ls.namespace = <nsRef>
      AND ls.values && ?            -- array overlap; omitted for the * wildcard
      AND rp.cluster_id = <clusterRef>
      AND rp.report_period_start >= ?
      AND rp.report_period_start < ?
  )
  OR EXISTS ( /* the storage-volume twin */ ))
AND EXISTS (
    SELECT 1 FROM <schema>.reporting_enabledtagkeys etk
    WHERE etk.key = ? AND etk.enabled   -- the folded disable fix (#815 comment)
  )
```

Multiple filters AND, as today. The wildcard (`*`) matches any value for the
key (`ls.key = ?` only).

**Four sites**, each keeping its outer correlation unchanged and swapping the
inner tag-match fragment: `applyDBTagFiltersToKeys` (gorm `?`),
`applyDBTagFiltersToClusterNamespace` (gorm `?`, parameterized columns),
`tagFilterDBExistsClause` (raw `$N` + arg counter),
`tagFilterDBExistsClauseCommaNamespaces` (raw, comma-separated namespaces).
One shared fragment builder with two arg-style adapters. The api-mode
siblings are untouched.

**Value semantics note:** the summary rows are per (key, period, namespace,
node) with a values array; a (cluster, namespace) matches if ANY node's row
in ANY window period carries a requested value — the same union the push reads (ADR-0353) — with one documented
divergence: the push then COLLAPSES multi-value keys to the alphabetically
first value for resolved_tags, while db mode matches the whole union
(see the review findings below; #814 owns the multi-value contract).

**Disable fix (folded):** the `enabledtagkeys` EXISTS makes a disabled key
stop matching immediately in db mode (today it matches until the next
summarization purges it) — matching api mode's full-replace semantics.

**Window helper:** computed once per query in Go
(`lastMonthStart`, `nextMonthStart` from `time.Now().UTC()`).

## Tests

Executed against the testcontainer with the tenant schema created ad-hoc
(the `db_provider_test.go` pattern — the reporting tables do not exist in
ROS's migrations):

1. **The RED (the issue's repro shape):** two clusters sharing
   `environment=production`, both periods in-window; label summaries
   seeded as ground truth (both clusters); `reporting_ocptags_values`
   seeded as summarization leaves it (last-run snapshot, cluster B only).
   The db-mode filter must match BOTH clusters.
   **Baseline: only cluster B — RED** (failure message names cluster
   exclusion, not a count mismatch).
2. **The window:** a cluster whose only period is >2 months old must NOT
   match (the ADR-0353 month-snap); a cluster with a period in the
   previous month MUST match.
3. **The storage source:** a key/value present only in the storage-volume
   summary matches (both sources feed the predicate).
4. **The disable fix:** a key disabled in `reporting_enabledtagkeys`
   stops matching immediately (no summarization run needed).
5. **The db≡api assertion (close criterion 3):** the same org data seeded
   both ways — label summaries for db mode, `org_container_keys.resolved_tags`
   for api mode — the two predicates select the same (cluster, namespace)
   set.
6. **The wildcard:** `key=*` matches any value.
7. **Existing SQL-shape tests updated:** the `reporting_ocptags_values`
   assertions become label-summary assertions; balanced-parentheses and
   arg-counting checks retained per site.

## Benchmark (measured, per the owner's ask)

Synthetic pessimistic org on the shared testcontainer (2 clusters × 150
namespaces; the filtered key on every namespace × 2 nodes × both window
periods × both label sources; 30 background keys; the 200-enabled-key
cap respected), 50 iterations, both sources over the same org data:

| Source | Per filtered list query |
|---|---|
| Old (`reporting_ocptags_values`) | ~0.37 ms |
| New (label summaries) | ~3.2 ms |

~8.5× the old predicate, bounded and once per query (the planner resolves
the correlated EXISTS as a hashed semi-join; the dominant cost is the
key-indexed scan of the filtered key's summary rows in the window) — the
same magnitude as one more keys-table scan on a list query that already
scans org-wide tables. The old-source number is its BEST case — the
arrays holding every cluster, which is exactly what they fail to do in
production (the bug). If this ever needs optimizing, the path is an
explicit materialized (cluster, namespace) CTE per query — no caching
(that would need owner approval).

## Mutations run

- **M1** (a site reverted to tags_values): the RED itself — baseline
  returns only the last-summarized cluster.
- **M2** (the folded disable EXISTS dropped): the disable test fails —
  the disabled key keeps matching, with the mechanism-naming message.
- **M3** (the window bounds dropped): the window test fails — the
  out-of-window cluster matches.

## Development findings (found live, fixed, and pinned by tests)

- **An argument evaluation-order bug** in the fragment builder: etk's
  placeholder was generated before the summary predicates ran (they run
  during `fmt.Sprintf`'s argument evaluation), appending its argument
  first and scrambling every bind after it. Also: gorm's `?` binds one
  arg per occurrence, so a placeholder cannot be shared between the pod
  and storage EXISTS (pgx's `$N` could repeat; one style must serve
  both). Fixed by building the summary predicates first; every
  occurrence gets its own placeholder and argument.
- **An off-by-one in the raw `$N` sites**: the pgx builder pre-increments,
  so passing it the next position made it skip one — `$2` never appeared
  while its argument sat in the list ("could not determine data type of
  parameter $2"). Found by the API suite (nodes/mig/pvcs routes), not
  the model tests — the raw sites had zero executed coverage. Fixed;
  both raw sites now have executed regression tests pinning the
  placeholder numbering AND the next-free-position convention the
  callers' argIdx arithmetic continues from (last used + 1).
- **Test-fixture consequences:** the per-test truncate only touches the
  public schema, so the tenant-schema fixture drops and recreates its
  schema; `SetupTestDB` holds the shared-DB mutex per test, so a test
  cannot call it twice (the first db≡api draft deadlocked). Three
  existing API fixtures seeded only the old table — extended to create
  the label-summary tables (the db-source integration test also seeds
  them now).

## Koku-side filing

**#867** — the arrays' per-run overwrite (merge or rebuild-from-window,
plus the stale-entry decision for clusters that stop reporting). Follows
koku's review path per the fork strategy.

## Fresh-context review (no P1; one P2 and five P3s — the P2 and the actionable P3s handled)

- **P2** (the folded disable EXISTS was not scoped by `provider_type` —
  an OCP-disabled key with an AWS-enabled row of the same name kept
  matching; koku's own SQL and ROS's DBTagProvider both scope it):
  **fixed** — the etk EXISTS now scopes `provider_type = 'OCP'`, pinned
  by `DisableScopedToOCP` (reviewer-verified by execution before the
  fix).
- **P3** (session-TZ hazard: timestamptz window args vs the DATE column
  shift the boundary under non-UTC sessions): **fixed** — window bounds
  bind as DATE strings with `::date` casts.
- **P3** (gorm site 2 — the parameterized-columns site — had no executed
  test): **added** `TestApplyTagFiltersToClusterNamespace_DBMode_Executed`.
- **P3** (the db≡api test could pass vacuously on empty-vs-empty):
  **fixed** — a non-emptiness guard (`require.Len(dbPairs, 2)`).
- **P3** (multi-value divergence: the push collapses to the
  alphabetically first value, db matches the whole union — "db≡api by
  construction" does not hold for multi-value keys): **accepted and
  documented** — db's behavior is arguably more correct, #814 owns the
  multi-value contract, and tag-filtering.md now states the divergence.
- **P3 noted, no action:** the db-mode tag catalog (`GetTagValues`) still
  reads `reporting_ocptags_values` (the picker, not the filter — same
  misleading table; a follow-up candidate now that #867 tracks the
  koku side); and `ParseKokuTagFilterParams` has no per-request filter
  cap (no demonstrated blowup; a cap is a new limit needing an owner
  decision).

The reviewer independently verified: the RED on baseline (stash-rerun);
mutation M3 re-run; placeholder/arg alignment end-to-end at a non-unity
base with two multi-value filters (executed probe — consecutive `$N`,
next-free convention, correct results); gorm errors loudly on
placeholder/arg mismatch (misalignment cannot silently mis-bind);
injection safety (schema digits-only + sanitized, all column refs from a
fixed set, values bound); the window equivalence (this_month_end is
midnight of the last day — the half-open form is equivalent for ANY
date, aligned or not); the disable-absent case (no etk row → no match in
both modes); the benchmark methodology and numbers (re-ran: ~0.57 ms vs
~2.9 ms — same magnitude); no vacuous fixture passes; and all three
development findings genuinely fixed.

## Deliverables

- The predicate rewrite (`internal/model/tag_filters.go` — four sites, one
  shared fragment builder, the window helper).
- Tests (executed RED + window + storage + disable + db≡api + wildcard;
  updated shape tests).
- The benchmark (a `_bench_test.go` + the measured numbers in the report).
- `docs-site/features/tag-filtering.md` — the db-mode source's semantics
  (label summaries, month-snapped window, immediate disable) and the
  remaining differences from api mode (timing: query-time vs pushed).
- The koku issue (filed, linked).
- CHANGELOG entry; this plan doc; completion report per
  properly-implement-feature.
