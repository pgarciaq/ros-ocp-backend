# ADR-0358: Cluster liveness and stale recommendation retention

## Status

Accepted (implements [#874](https://github.com/pgarciaq/ros-ocp-backend/issues/874))

## Phase

Data lifecycle — stale-state semantics and retention of recommendation tables

## Context

`recommendation_sets.stale` is written in exactly one place:
`MarkUnreportedContainersStale`, which runs at the end of a cluster's *own*
processing cycle (`librobne/pgrec/write.go:141`; callers
`internal/services/report_processor.go:388` and
`internal/engine/threshold_recalculate.go:388`, the latter skipped when the run
wrote nothing). A cluster that stops reporting never runs a cycle, so it is
never marked.

Three consequences follow, all reachable by code read:

1. `org_container_keys.is_stale` copies that flag, so the key catalog never
   learns either.
2. Retention deletes only `stale = true` rows
   (`purgeStaleRecommendations`, `internal/engine/retention.go:302`), so nothing
   is ever reclaimed for a cluster that stopped reporting.
3. Default lists exclude stale rows (`internal/api/utils.go:881`), fleet and
   savings SQL hardcode `stale = false`
   (`internal/api/handlers_fleet.go:98`, `handlers_savings_summary.go:519`), and
   `org_recommendation_stats` counts them — so a decommissioned cluster keeps
   being listed, counted and included in savings.

`namespace_recommendation_sets` has no marking twin at all: its `stale` column
is written only by the produce path from digest age
(`librobne/namespace/recommend.go:92`), and `ns.stale` is already wired into the
list filter (`internal/model/native_ns_list_keys.go:33`) while remaining false
for a cluster that stopped reporting.

The natural clock already exists and is already read by the engine:
`clusters.last_reported_at`, set per processed report
(`internal/services/report_processor.go:231`), indexed
(`migrations/000015`), and consumed by `engine.IsStaleRecommendation`
(`librobne/engine/helpers.go:131`). What is missing is evaluating that predicate
for clusters that do not run.

Koku's only comparable rule is `check_for_stale_ocp_source`
(`koku/masu/celery/tasks.py:417`): a 3-day threshold that fires a notification
(`event_type=cm-operator-stale`) and takes no state action. Upstream never marks,
filters or deletes on staleness, so there is no parity precedent for a retention
rule — ROS owns the number.

## Decision

### Liveness clock

`clusters.last_reported_at`, with a window of **14 days**
(`ROS_CLUSTER_LIVENESS_HOURS`, default `336`). `ROS_MAX_LOOKBACK_DAYS` (90 d)
keeps its single meaning — the window the engine reasons over — and is not read
by the sweep.

### Marking writes `staled_at`, never `updated_at`

`updated_at` is the report time: the container list projects it as
`last_reported` (`internal/model/common.go:49`), and namespace lists project
`clusters.last_reported_at` for the same field
(`internal/model/common.go:92`) — a pre-existing two-source split recorded in
ADR-0357. A sweep that bumped `updated_at` would make a dead cluster report
"last reported: today".

Both recommendation tables gain `staled_at TIMESTAMPTZ NULL`. Three writers set
`stale = true`, and each records the clock with the semantics its episode needs:

| Writer | Statement | `staled_at` |
|--------|-----------|-------------|
| Per-cycle mark (`MarkUnreportedContainersStale`, `librobne/pgrec/write.go:143`) | `UPDATE … SET stale = true, staled_at = COALESCE(staled_at, now())` | first mark wins, so repeated cycles cannot extend the window |
| Sweep mark (`markAbandonedClustersStale`, `internal/engine/retention.go:495`) | same shape | same: the deadline runs from the first sweep that noticed |
| Upserts (`librobne/pgrec/write.go:72`, `librobne/pgrec/namespace.go:64`) | `staled_at = CASE WHEN EXCLUDED.stale THEN now() ELSE NULL END` | a row that arrives already stale starts a fresh clock; a live row clears it |

So a resumed cluster clears the mark and its clock in the same statement, while a
re-mark of a still-silent cluster keeps the original deadline.

### Deletion is keyed on `staled_at` and re-verifies liveness

`purgeStaleRecommendations` deletes on
`COALESCE(staled_at, updated_at) < cutoff AND NOT EXISTS (recent clusters row)`
for both tables. `COALESCE` gives rows that were already stale — for which no
backfill is possible in a test harness that migrates an empty database — the
deadline they have today. The `NOT EXISTS` is the guard on the irreversible
step: a cluster that is in fact reporting survives a mistaken mark.

The visible stale window is therefore `ROS_STALE_CLEANUP_DAYS` (30 d) from
marking, independent of the liveness value: mark at day 14, delete at day 44.

## Alternatives Considered

- **90 days (`ROS_MAX_LOOKBACK_DAYS`) as the liveness window.** Principled —
  "gone when no engine input could describe it" — but a decommissioned cluster
  would inflate fleet totals, savings and recommendation counts for a full
  quarter, and `staled_at` had already removed the coupling that made the long
  window necessary.
- **Bump `updated_at` when marking** (today's writer behaviour). Makes
  `last_reported` lie for dead clusters, and already misreports live clusters
  whose container merely missed a cycle.
- **A global "no cluster reported anywhere" guard.** Withdrawn: its condition is
  circular (it uses the symptom the sweep acts on as its own precondition), its
  scope is fleet-global while the sweep is per-org (so it cannot see one
  tenant's broken ingest path), and on a single-cluster install it makes the
  sweep a permanent no-op.
- **A sleep exemption for ADR-0348 / #714.** `hcp_sleep_evidence` is written
  from reports (`internal/ingestion/sleep_evidence.go:187`), so it stops
  advancing exactly when a cluster sleeps; it cannot separate a long sleep from
  death. Intermittent sleep never approaches the window, so the threshold covers
  it, and deletion is bounded: only the two recommendation tables are removed,
  history and node/PVC/snapshot rows survive, and a resumed cluster repopulates
  within one cycle.
- **A separate CronJob owner.** Unnecessary: native CSV ingest itself is
  `!EnabledFor(Kruize)` (`internal/services/report_processor.go:240`), so the
  retention ticker — skipped under Kruize (`cmd/start.go:95`) — is mode-correct.

## Consequences

- A cluster silent for more than 14 days disappears from default lists and from
  fleet, savings and recommendation counts; after 30 more days its
  recommendation rows are deleted. Its `clusters` row, tags and
  cluster-quota settings are untouched, so Koku's cluster picker keeps listing it
  and a resumed cluster is immediately recognizable — the sweep never deletes
  provenance, only recommendations. Node recommendations, node utilization,
  history, PVC, snapshots and GPU MIG rows also survive.
- An ingestion outage longer than 14 days hides the affected clusters until each
  reports again. The mark is reversible (`stale = EXCLUDED.stale`), and
  `filter[stale]=only` still lists them until the purge. (`filter[stale]=true`
  means "no filter": it returns live and stale rows together, which is why the
  escape hatch is spelled `only`.)
- A cluster registered but never reported (`last_reported_at IS NULL`) is never
  marked: it is unknown, not abandoned, and marking it would make a fresh
  registration invisible.
- The mark and the purge both refresh the org's list key catalogs
  (`org_container_keys`, `org_namespace_keys`) and invalidate the fleet caches.
  Without the mark refresh, `meta.count` outruns the rows the list can return;
  without the purge refresh, the stale view keeps counting rows that no longer
  exist, and an org whose only cluster died never runs another ingest to repair
  it.
- Node recommendations, node utilization, history, quota, PVC, GPU MIG and VM
  recommendations do not read `stale` and keep the cluster. Bringing them in
  would need a `stale` column first, not a filter.
- `ROS_CLUSTER_LIVENESS_HOURS` is independent of `ROS_STALE_CLEANUP_DAYS`; the
  gap between them is the visible stale window.
- Every counter on the fleet summary card excludes stale rows —
  `total_containers` and `cluster_count` included (owner decision, 2026-11-18,
  revising an earlier "keep totals raw" call). The card divides
  `idle_containers` and `abandoned_containers` by `total_containers`, so a
  filtered numerator over an unfiltered denominator would understate idleness by
  exactly the dead cluster's weight, and `abandoned_containers` could exceed the
  total it is divided by. `total_containers > 0` also drives the UI's has-data
  check, so a fleet with no live cluster renders no card instead of a phantom
  one. Known consequence: these counts are as reversible as the stale flag, and
  Koku's cluster picker (Koku-owned `resource-types/openshift-clusters/`) keeps
  listing the dead cluster for up to `ROS_STALE_CLEANUP_DAYS`.

## Verification

Each claim below has a test that fails pre-fix. Where the mutation was run, the
failure message names the mechanism, not an incidental count.

| Claim | Test | Mutation that fails it |
|-------|------|------------------------|
| A silent cluster leaves the fleet summary in one sweep | `TestRunRetentionSweep_MarksAndHidesAbandonedCluster` (`internal/api`) | unfiltering any fleet counter |
| The mark reaches the key catalogs, so `meta.count` equals the rows returned and `filter[stale]=only` sees the marked rows | `TestRunRetentionSweep_KeepsListCountAndRowsAgreeing` (`internal/api`), both lists | dropping `refreshOrgListCatalogs` from the mark: count 3 vs rows 1, `stale=only` 0 |
| The purge prunes catalog entries of deleted rows | `TestPurge_PrunesOrgListCatalogs` (`internal/engine`) | dropping the purge refresh: `org_container_keys` keeps the orphan |
| Marking never moves `updated_at` (report time) | `TestMarkAbandonedClustersStale_MarksBothTables` | adding `updated_at = now()` to the mark: "marking must not move updated_at" |
| A mistaken mark survives the purge, on both tables | `TestPurge_KeepsMistakenlyMarkedCluster` | removing the `NOT EXISTS` guard |
| A destroyed cluster (no `clusters` row) is still reclaimed | `TestPurge_ReclaimsRowsOfDestroyedCluster` | turning the guard into a join |
| The per-cycle writer leaves `updated_at` untouched | `TestMarkUnreportedContainersStale_PreservesUpdatedAt` | restoring the old `updated_at = now()` |

Plans: the purge drives off `idx_ros_rs_org_cluster_stale_updated_at`
(`Index Cond: stale = true`) with a hash anti-join against `clusters`; the mark
is a sequential scan of non-stale rows joined to `clusters`, once per 24-hour
tick — recorded in `docs/operations/retention.md` rather than asserted here.

## Cross-Repo Dependencies

None. No chart, operator or Koku change; the sweep runs inside the retention
sweep the processor already performs.

## Related Decisions

- ADR-0298 — composite-key sweep and stale detection (the per-cycle writer this
  decision completes).
- ADR-0132 — per-table retention policies (the purge this decision keys on
  `staled_at`).
- ADR-0353 — tag push and row window; #820 decided that tag filtering carries no
  liveness predicate, so this rule applies to lists and aggregates only.
- ADR-0348 — non-prod sleep advisory (expected absence, covered by the window).
- ADR-0357 — `last_reported` is `recommendation_sets.updated_at` on the
  container path and `clusters.last_reported_at` on the namespace path.
