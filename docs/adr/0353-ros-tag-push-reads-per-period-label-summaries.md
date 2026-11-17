# ADR-0353: ROS Tag Push Reads Koku's Per-Period Label Summaries

## Status

Accepted — implemented in #770 (Koku `masu/processor/ros_tag_sync.py`).
Amended 2026-11-17: `db` mode moved to this source and window (#815), and the
`reporting_ocptags_values` arrays became a window rebuild rather than a per-run
snapshot (#867).

## Phase

Tag filtering — Koku → ROS push sync payload source and window

## Context

The Koku-side push builder (`masu/processor/ros_tag_sync.py`) read
`OCPUsageLineItemDailySummary.all_labels` for a single tenant-wide day: the
maximum `usage_start` across all of the org's OCP line items. ROS then applies
the payload with an org-scoped **full replace** (ADR-0120): it resets
`org_container_keys.resolved_tags` to `{}` and applies only what arrived.

The two behaviours combine into data loss. Clusters do not all summarize the
same day — a cluster whose most recent report is one day older than the org
maximum, or whose report lands late, is simply absent from the payload. Its rows
are still in ROS, they are just wiped by the reset, so tag filters stop matching
an entire cluster while every other cluster looks healthy. Nothing in the
response says why; the tags are gone.

Independently, Koku's own Tags API — the catalog behind the UI tag picker — never
read the daily summary. `api/tags/ocp/queries.py` declares two data sources,
`OCPUsagePodLabelSummary` and `OCPStorageVolumeLabelSummary`, and
`TagQueryHandler._set_start_and_end_dates` with `_get_time_based_filters` snaps
the default `time_scope=-30` request to whole months. So ROS's notion of "which
clusters and values exist" was built from a different table and a different
window than Koku's, and the two disagreed in production.

## Decision

1. **Source.** The payload is built from the two label-summary tables the Tags
   API declares — `reporting_ocpusagepodlabel_summary` and
   `reporting_ocpstoragevolumelabel_summary` — not from
   `OCPUsageLineItemDailySummary.all_labels`. Pod and storage labels are merged
   the same way the Tags API merges its two sources.
2. **Window.** Report periods whose `report_period_start` falls in
   `[last_month_start, this_month_end]` (the Tags API's month-snapped default).
   Label-summary rows accumulate within a report period, so a cluster keeps its
   tags for as long as one of its periods is inside the window, regardless of
   which day it happened to summarize.
3. **Aggregation.** Values are unioned per `(cluster_id, namespace, key)` in
   Python, then collapsed to `sorted(values)[0]` because `resolved_tags` holds a
   single value per key. The collapse is deterministic rather than
   last-row-wins. Multi-value storage is a separate contract change (#814).
4. **Payload shape and consumer unchanged.** The JSON envelope, the
   `org_container_keys.resolved_tags` storage, and the ROS full-replace
   transaction stay exactly as ADR-0120 defines them.
5. **No recompute on toggle.** Enabling or disabling tag keys still only
   schedules a push. Multiple toggles produce one catalog update on the next
   sync; nothing repopulates summary tables per click.

## Alternatives Considered

- **Age-bounded scan of the daily summary** (e.g. last N days instead of the
  latest day) — rejected: still a single shared window over a fact table that
  has no row at all for a cluster that has not reported recently, which is the
  failure being fixed.
- **Per-cluster latest period** (`MAX(usage_start)` grouped by `cluster_id`) —
  rejected: it diverges from the Tags API (the thing ROS is supposed to mirror),
  multiplies the query per cluster, and has no exit rule — a cluster that stops
  reporting would keep its tags forever.
- **Repopulate summaries when a key is enabled** — rejected: it reintroduces the
  per-click recompute that batching exists to prevent. This was considered and
  dropped before implementation.
- **Emit value arrays and teach ROS to store them** — deferred to #814; that is
  a ROS storage and filter-semantics change, not a payload-source change.

## Consequences

- **Month-granular stickiness.** A cluster that stops reporting keeps its tags
  until its last report period leaves the window — up to roughly two months.
  This is the deliberate cost of not dropping a live cluster's tags on a
  calendar boundary.
- **Batching is visible, not a lag.** A newly enabled key appears in `tag_keys`
  immediately (the catalog is built from enabled keys), but namespace values
  arrive with the cluster's next summarized report. Several toggles produce one
  catalog update. This is the intended behaviour, and it now matches Koku's
  UI cadence instead of bypassing it.
- **Payload size is O(keys × namespaces × nodes) per period in the window.** The
  query selects only the columns the payload needs, but the row count is not
  small. It was not measured against a large tenant; a synthetic large-tenant
  measurement is the next step if push duration becomes a concern.
- **`ROS_TAGS_SOURCE=db` diverges — superseded 2026-11-17.** db mode used to read
  `reporting_ocptags_values`, a per-run last-writer snapshot rebuilt from one
  provider's report periods. It read a different table, over a different window,
  with a different cluster set than the push path. Both halves are now closed:
  #815 moved the db reader onto this ADR's source and window, and #867 rebuilt
  the catalog arrays — see *Amended (2026-11-17)* below.
- **Cluster-ID drift is safe.** Report periods are created with `get_or_create`
  on `(cluster_id, report_period_start, report_period_end)` and only
  `cluster_alias` is ever rewritten, so a changed cluster ID creates a new
  period rather than retagging old rows under the new ID.

## Values Are Never Pruned (decided 2026-11-07)

Koku's label summaries **accumulate** values within a report period: the pod and
volume SQL write the stored array back as a union with the new run
(`unnest(va."values" || coalesce(ls."values", '{}'::text[]))`), and the only
`DELETE` in either file removes rows for keys whose
`reporting_enabledtagkeys.enabled` is false. Combined with the two-period window,
a value that stops appearing on a workload keeps matching it for **up to two
monthly periods**, and a relabeled namespace can match both its old and new
value for that long (the api payload keeps one of them —
`sorted(values)[0]` — so which one wins is arbitrary, not last-writer).

**Decision: accept it as the contract.** It is now stated in
`docs-site/features/tag-filtering.md#tag-lifecycle`, and the reasons the
alternatives lost are:

- **Prune per period (Koku).** The same tables feed Koku's Tags API catalog and
  the OCP-on-cloud `matched_tags` SQL, which joins `reporting_ocptags_values` to
  provider tag values on **key and value** for cost allocation. A mid-period
  prune would withdraw value matches for earlier days of the same period, so
  cost allocation could change retroactively within a period.
- **Per-cluster latest summarized day (value whitelist).** A namespace with no
  line item on that cluster's most recent day — idle this week, or a partial or
  late-arriving report — loses all of its labels. That is #770's failure class
  again, at namespace granularity and on a much shorter clock.
- **A ROS-side recency gate.** For *values* it is not implementable where the
  dates are missing: `org_container_keys.resolved_tags` is a flat map with no
  per-value timestamps, and `reporting_ocptags_values` carries
  `cluster_ids`/`namespaces` arrays but no dates, so any value-recency rule has
  to run in the builder — inheriting the two objections above. A *row* recency
  gate is a different, implementable choice: `org_container_keys.last_reported`
  freezes when a cluster stops being processed, and `is_stale` records a missed
  cycle for clusters that still are. Neither addresses value staleness, so they
  are tracked separately in #820.

**What would reopen this:** a tenant where a value must stop matching
immediately, e.g. a key removed for compliance reasons, or users reporting stale
tag matches after a relabel.

## Amended (2026-11-17): db mode reads this source, and the catalog is rebuilt

Two changes closed the db-mode divergence recorded above.

**#815 — the db reader moved onto this ADR's source and window.** Every
`ROS_TAGS_SOURCE=db` filter site now matches a container's
`(cluster_uuid, namespace)` against `reporting_ocpusagepodlabel_summary` ∪
`reporting_ocpstoragevolumelabel_summary`, joined through
`reporting_ocpusagereportperiod` for the cluster and restricted to report
periods whose `report_period_start` is inside `[last_month_start,
this_month_end]`. The sites are `MatchingContainerKeys`,
`ApplyTagFiltersToKeys`, `ApplyTagFiltersToClusterNamespace`,
`ApplyTagFiltersToNSKeys`, `TagFilterExistsClause`, and
`TagFilterExistsClauseForCommaSeparatedNamespaces`; all six call one predicate
(`labelSummaryTagMatch`). `db` and `api` now select the same clusters from the
same tables. The predicate also checks `reporting_enabledtagkeys`
(`enabled`, `provider_type='OCP'`) at query time, so a disabled key stops
matching immediately — decision 5 still holds, because nothing is recomputed on
toggle.

**#867 — `reporting_ocptags_values` is rebuilt, not overwritten.** The pod and
volume label-summary SQL now recompute `cluster_ids`, `cluster_aliases`,
`namespaces`, and `nodes` for the `(key, value)` pairs the run touched, from
both label summaries over the same window, instead of letting the summarizing
cluster's arrays replace the row. A touched pair with no cluster left inside the
window is deleted rather than kept with empty arrays (an empty array satisfies
every `contained_by` filter). The table is the tag catalog the picker and the
OCP-on-cloud `matched_tags` SQL read; no ROS **filter** reads it.

**Why two ROS sites had to change with #867.** The container-key
(`MatchingContainerKeys`, pgx) and namespace-key (`ApplyTagFiltersToNSKeys`,
gorm) sites read the catalog with `unnest(tv.cluster_ids, tv.namespaces)`,
which pairs the two arrays **by position**. Under the per-run snapshot both
arrays were written together for one cluster, so positions happened to agree —
the defect was that the other clusters were missing, not that pairs were wrong.
Under the rebuild the two arrays are aggregated independently over the whole
window, so their orders are unrelated: `cluster_ids = [A, B]` with
`namespaces = [ns-b, ns-a]` yields `(A, ns-b)` and `(B, ns-a)`, neither of which
carried the tag. That converts a silent miss into a **false match** inside the
same org. Both sites now use the label-summary predicate, which needs no
pairing.

**Executed coverage.** `TestMatchingContainerKeys_DBMode_NoPositionalArrayPairing`
and `TestApplyTagFiltersToNSKeys_DBMode_NoPositionalArrayPairing`
(`internal/model/tag_filters_db_exec_test.go`) seed that cross-ordered catalog
plus the key rows for both false pairs, and assert only the label summaries'
true pairs match. Reverting either site to `unnest()` fails them with the false
pair present and the true pair missing.

**What would reopen this:** a db-mode query that has to answer "which cluster
and namespace" from the catalog arrays alone, where the label summaries are not
reachable.

## Verification

- Regression test `test_build_namespace_tags_payload_spans_clusters_and_report_periods`
  seeds two report periods (last month, this month), two clusters, both label
  sources, and the daily-summary rows the old builder read. Pre-fix it fails
  with `Items in the first set but not the second: ('ros-tag-cluster-a',
  'ros-tag-ns-a')` — naming the excluded cluster, not a count mismatch.
- Mutation-checked: dropping the storage source from the source tuple fails the
  test on the missing `app` value; restoring it passes.
- `masu.test.processor.test_ros_tag_sync` — 12/12 pass; flake8 clean.
- `api.settings.test.tags.test_ros_tag_sync_trigger` fails 2 tests with
  `204 != 403` at HEAD with this change stashed — pre-existing, unrelated.

## Cross-Repo Dependencies

| Repo | Artifact | Contract |
|------|----------|----------|
| koku | `koku/masu/processor/ros_tag_sync.py` (producer) | Builds the payload from label summaries in the month window |
| ros-ocp-backend | `internal/tags/sync.go` (consumer) | Full replace of `org_container_keys.resolved_tags`; unchanged |
| koku | `reporting_ocpusagepodlabel_summary`, `reporting_ocpstoragevolumelabel_summary` | Table and column semantics this ADR depends on |
| ros-ocp-backend | `ROS_TAGS_SOURCE=db` reader | Covered by #815 (same source and window) and #867 (catalog rebuilt, no positional pairing) |

## Related Decisions

- [ADR-0120](0120-saas-http-push-tag-sync.md): the push transport, envelope, and
  full-replace semantics this record keeps.
- [ADR-0054](0054-resolved-tags-jsonb-on-keys-table.md): `resolved_tags` holds a
  single value per key — the reason for the deterministic collapse.
- [ADR-0226](0226-tag-sync-full-replace-per-org.md): full replace is what turns a
  missing cluster into a wiped one, and what this record makes survivable.
