# ADR-0298: Composite-key sweep for stale detection

## Status

Accepted

## Phase

14

## Context

The existing stale detection algorithm ([ADR-0224](0224-stale-marking-precedence-last-reported-at-overrides-digest-age.md))
uses only cluster-level `last_reported_at` to determine whether recommendations
are stale. This handles "cluster stopped reporting" but misses a subtler case:
when a container's **composite key** changes.

The composite key for `recommendation_sets` is:

```
(org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine)
```

When any key component changes — for example `workload_type` moves from
`deployment` to `statefulset` — the `ON CONFLICT` upsert in
`WriteRecommendations` creates a **new row** (the new composite key) but the
**old row** is never overwritten and never marked stale. It lingers with
`stale = false` indefinitely, showing no plots data in the UI because no new
digests match its composite key.

This is invisible to cluster-level staleness because the cluster itself is still
actively reporting.

## Decision

Add a post-reconcile sweep (`MarkUnreportedContainersStale`) that marks
`recommendation_sets` rows stale when their `updated_at` timestamp was not
refreshed during the current reconcile cycle.

**Mechanism:**

`WriteRecommendations` already sets `updated_at = now()` on every row it
upserts. After a full reconcile cycle completes for a given org+cluster, any
non-stale row whose `updated_at < cycleStart - 5min` was not refreshed — its
composite key no longer matches any active digest data.

**Grace window:** 5 minutes accounts for clock skew between application servers
and transaction commit delays during large reconcile batches.

**Wiring:**

- Kafka ingestion path: called at the end of
  [`ProcessReport`](../../internal/processor/report_processor.go) after all
  containers for a cluster payload are upserted.
- Threshold recalculation path: called at the end of
  [`RecalculateThresholds`](../../internal/engine/threshold_recalculate.go)
  after the full org reconcile completes.

**Implementation:** A single `UPDATE` query in
[`MarkUnreportedContainersStale()`](../../internal/engine/recommend_all.go) —
O(1) regardless of container count (the database performs an index scan on
`(org_id, cluster_uuid, stale, updated_at)`).

**Edge cases:**

- **Container temporarily disappears and returns:** Marked stale by the sweep,
  then upserted fresh (stale cleared) on next appearance.
- **Multiple workload_type changes:** Each cycle only keeps the currently-active
  composite key; all prior variants are swept.
- **Retention:** Stale rows are deleted after 30 days by the existing retention
  sweep ([ADR-0132](0132-retention-policies-per-table.md),
  [ADR-0203](0203-retention-side-effects-beyond-partition-drop.md)).
- **5-minute grace window:** Prevents false positives from clock skew and
  transaction delays during large batch reconciles.

## Alternatives Considered

### Explicit delete on composite-key mismatch

Delete the old row when a new composite key is detected during upsert. Rejected:
requires tracking all prior keys per container to identify which rows to delete,
adds complexity to the hot write path, and loses the stale → retention lifecycle
that operators and auditors rely on.

### Trigger-based approach (PostgreSQL trigger)

Fire a trigger on `recommendation_sets` INSERT that marks rows with the same
logical container but different composite key. Rejected: trigger logic would need
to define "same logical container" without using the composite key itself
(circular), adds latency to the critical ingest path, and is harder to test.

### Periodic background job (housekeeper)

Run the sweep in the housekeeper process on a timer. Rejected: decouples the
sweep from the reconcile cycle, introducing a window where stale rows are
visible in the API. The sweep must run immediately after reconcile to maintain
consistency.

## Consequences

### Positive

- Old composite-key rows no longer linger with `stale = false` indefinitely.
- UI stops showing empty-plots recommendations for renamed/redeployed workloads.
- Complements cluster-level staleness — no gap between "cluster down" and
  "container key changed" scenarios.
- Single UPDATE query adds negligible overhead to the reconcile cycle.
- Stale rows follow the existing retention lifecycle (30-day cleanup).

### Negative

- Containers that temporarily disappear for one cycle are marked stale until
  they reappear. This is correct behavior (the recommendation is stale until
  fresh data arrives) but may surprise operators watching short-lived jobs.
- The 5-minute grace window is a tuning constant; extremely long-running
  reconcile batches (>5 minutes of clock drift) could theoretically false-positive,
  though this is unlikely in practice.

## Related Decisions

- [ADR-0224](0224-stale-marking-precedence-last-reported-at-overrides-digest-age.md): Cluster-level staleness precedence.
- [ADR-0161](0161-staleness-threshold-hours-alias.md): Staleness threshold env alias.
- [ADR-0225](0225-filter-stale-tri-state-semantics.md): API stale filter tri-state semantics.
- [ADR-0255](0255-org-container-keys-refresh-deletes-stale.md): org_container_keys refresh deletes stale keys.
- [ADR-0289](0289-defer-org-metadata-refresh-end-of-reconcile.md): Defer org metadata refresh to end of reconcile.

## References

- [internal/engine/recommend_all.go](../../internal/engine/recommend_all.go) — `MarkUnreportedContainersStale()`
- [internal/processor/report_processor.go](../../internal/processor/report_processor.go) — Kafka ingestion wiring
- [internal/engine/threshold_recalculate.go](../../internal/engine/threshold_recalculate.go) — Threshold recalc wiring
- [docs/operations/stale-detection.md](../operations/stale-detection.md) — Operational documentation
