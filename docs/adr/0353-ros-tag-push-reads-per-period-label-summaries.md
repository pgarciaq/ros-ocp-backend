# ADR-0353: ROS Tag Push Reads Koku's Per-Period Label Summaries

## Status

Accepted — implemented in #770 (Koku `masu/processor/ros_tag_sync.py`)

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
- **`ROS_TAGS_SOURCE=db` still diverges.** db mode reads
  `reporting_ocptags_values`, a per-run last-writer snapshot rebuilt from one
  provider's report periods. It was already different from the push path and
  remains so; tracked in #815.
- **Cluster-ID drift is safe.** Report periods are created with `get_or_create`
  on `(cluster_id, report_period_start, report_period_end)` and only
  `cluster_alias` is ever rewritten, so a changed cluster ID creates a new
  period rather than retagging old rows under the new ID.

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
| ros-ocp-backend | `ROS_TAGS_SOURCE=db` reader | Not covered — see #815 |

## Related Decisions

- [ADR-0120](0120-saas-http-push-tag-sync.md): the push transport, envelope, and
  full-replace semantics this record keeps.
- [ADR-0054](0054-resolved-tags-jsonb-on-keys-table.md): `resolved_tags` holds a
  single value per key — the reason for the deterministic collapse.
- [ADR-0226](0226-tag-sync-full-replace-per-org.md): full replace is what turns a
  missing cluster into a wiped one, and what this record makes survivable.
