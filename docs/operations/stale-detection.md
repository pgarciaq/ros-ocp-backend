# Stale Detection

This document describes how ROS-OCP-Backend marks container and namespace
recommendations stale, how the API exposes them, and how that differs from
VolumeSnapshot staleness classification.

## Definition

A **container** or **namespace** recommendation is **stale** when the source
cluster has not reported new usage data within the configured threshold. That
usually means the metrics operator stopped uploading, the cluster was
decommissioned, or there is a long network or ingress outage.

Snapshot recommendations use a separate age-based model; see
[snapshot staleness](../features-f-snapshot-staleness.md).

## Detection algorithm

During each recommendation run (`RecommendAllWorkloads`, `RecommendNamespaces`),
the engine sets the `stale` column when:

```text
now - reference_time > ROS_STALENESS_THRESHOLD_HOURS (default 48)
```

**Reference time (cluster takes precedence):**

1. **`clusters.last_reported_at`** — updated on each successful OCP report
   ingestion for that cluster. Used when non-zero. Reships and delayed uploads
   refresh this timestamp even when digest `bucket_date` values are historical,
   so a cluster that is still reporting is not marked stale because old digest
   rows remain in the database.
2. **Latest digest date** — fallback when `last_reported_at` is unknown: the
   most recent `interval_start` / bucket date for that container or namespace
   aggregate.

Implementation: [`isStaleRecommendation`](../../internal/engine/recommend_all.go),
[`StalenessThreshold`](../../internal/engine/recommend_all.go).

### Per-container composite-key sweep

The cluster-level check above handles "cluster stopped reporting." A separate
sweep handles "container's composite key changed" — when a key component
(`workload_type`, `workload`, `namespace`, etc.) changes, the ON CONFLICT upsert
creates a new row but the old row is never overwritten.

After each reconcile cycle completes for a given org+cluster,
[`MarkUnreportedContainersStale`](../../internal/engine/recommend_all.go) marks
any non-stale `recommendation_sets` row whose `updated_at` was not refreshed:

```text
UPDATE recommendation_sets
SET stale = true
WHERE org_id = :org_id
  AND cluster_uuid = :cluster_uuid
  AND stale = false
  AND updated_at < :cycleStart - interval '5 minutes'
```

`WriteRecommendations` sets `updated_at = now()` on every upsert, so rows with
active composite keys are always refreshed. The 5-minute grace window prevents
false positives from clock skew and transaction delays.

This runs in both the Kafka ingestion path
([`report_processor.go`](../../internal/processor/report_processor.go)) and the
threshold-recalculation path
([`threshold_recalculate.go`](../../internal/engine/threshold_recalculate.go)).

See [ADR-0298](../adr/0298-composite-key-sweep-stale-detection.md).

## Configuration

| Environment variable | Default | Description |
|---------------------|---------|-------------|
| `ROS_STALENESS_THRESHOLD_HOURS` | **48** | Hours without a cluster report before marking recommendations stale |
| `ROS_STALE_DATA_THRESHOLD_HOURS` | (alias) | Same as `ROS_STALENESS_THRESHOLD_HOURS` (requirements spec name) |
| `ROS_STALE_CLEANUP_DAYS` | 30 | Days before stale rows are deleted in the retention sweep |

See also [configurability](../architecture/configurability.md) and
[retention](retention.md).

## API filter

List endpoints support `filter[stale]` (legacy: `?stale=`):

| Value | Behavior |
|-------|----------|
| `false` or omitted | Exclude stale rows (default) |
| `true` | Include stale and non-stale |
| `only` | Only stale rows |

**Endpoints:**

- `GET /recommendations/openshift` — container recommendations
- `GET /recommendations/openshift/namespaces` (and legacy namespace list paths)

Detail endpoints continue to exclude stale rows by default so UIs do not surface
outdated guidance on drill-down unless the list is explicitly filtered.

## Notification

When `stale = true`, notification code **2** (`STALE_DATA`) is appended on **container and namespace** recommendations:

> No new metrics data received for more than 48 hours

Emitters: [`EvaluateNotificationsWithThresholds`](../../internal/engine/notifications.go) (containers), [`EvaluateNamespaceNotificationsWithThresholds`](../../internal/engine/recommend_namespace.go) (namespaces). Definitions: [`internal/notifications/mapping.go`](../../internal/notifications/mapping.go). Catalog API: `GET /api/cost-management/v1/recommendations/openshift/notification-codes`.

## Lifecycle

```text
Cluster reporting → last_reported_at refreshed → stale = false
        ↓ (no report for > threshold, default 48h)
stale = true, notification code 2
        ↓ (stale for > ROS_STALE_CLEANUP_DAYS, default 30d)
Deleted by retention sweep

Composite key changed (e.g., workload_type: deployment → statefulset)
        ↓ (new row upserted with new key; old row's updated_at not refreshed)
Post-reconcile sweep: old row → stale = true, notification code 2
        ↓ (stale for > ROS_STALE_CLEANUP_DAYS, default 30d)
Deleted by retention sweep
```

Fresh reporting clears `stale` and removes code 2 on the next recommendation run.

## Edge cases

- **Idle vs stale:** Idle workloads (low CPU/memory) still report metrics; they
  get idle notifications, not staleness.
- **Abandoned vs stale:** Zero usage for 72+ hours is code 8 (abandoned), not
  staleness.
- **Delayed uploads:** As long as ingestion updates `last_reported_at` within
  the threshold, recommendations stay non-stale even if digest dates lag.
- **Workload type change:** When a container's deployment type changes (e.g.,
  Deployment → StatefulSet), the new composite key receives a fresh
  recommendation row. The old row's `updated_at` is not refreshed by the upsert,
  so the post-reconcile composite-key sweep marks it stale on the same cycle.
- **Snapshot staleness:** VolumeSnapshot classifications (`orphaned`, `stale`,
  etc.) use `GET /recommendations/openshift/snapshots` and
  `ROS_SNAPSHOT_*` / `/settings/snapshot`, not this threshold.

## VolumeSnapshot cost estimates (separate from reporting staleness)

Snapshot **recoverable holding cost** is unrelated to `ROS_STALENESS_THRESHOLD_HOURS`
above. It is computed at ingestion from `restore_size_bytes` and a resolved
`cost_per_gib_month_usd` rate.

| State (v1) | What Ops/QE should expect |
|------------|---------------------------|
| **Today** | Placeholder economics: per-org `cost_per_gib_month_usd` in `/settings/snapshot`, optional `ROS_SNAPSHOT_COST_PER_GIB_MONTH_USD`, else Masu `effective_rates` `storage_gb_usage_per_month` (PVC usage proxy), else **$0.05**/GiB/month default |
| **Future** | Provider-accurate costs via Koku billing data and/or cost-model storage rates, delivered through the **effective cost internal endpoint** from **[COST-7523](https://redhat.atlassian.net/browse/COST-7523)** |

Do not treat v1 dollar fields as CUR-backed invoice amounts. Tune the flat rate for
demos and on-prem Ceph/ODF pools; wait for COST-7523 for SaaS CUR alignment.

Details: [snapshot staleness feature](../features-f-snapshot-staleness.md),
[cost-integration.md — Snapshot cost](../architecture/cost-integration.md#snapshot-cost-dynamic-default-from-effective-rates),
[docs-site snapshot staleness](../../docs-site/features/snapshot-staleness.md).

## Snapshot restore automation (v1 = detection only)

ROS v1 **classifies** VolumeSnapshots only. It does **not** run restore-and-verify,
automated safe-delete, or Velero/OADP backup workflows. Cleanup remains manual or
customer-owned automation against `GET .../snapshots` and namespace summary APIs.

## Related ADRs

- [ADR-0224](../adr/0224-stale-marking-precedence-last-reported-at-overrides-digest-age.md): Cluster-level staleness precedence (`last_reported_at` overrides digest age)
- [ADR-0298](../adr/0298-composite-key-sweep-stale-detection.md): Post-reconcile composite-key sweep
- [ADR-0161](../adr/0161-staleness-threshold-hours-alias.md): Staleness threshold env alias
- [ADR-0225](../adr/0225-filter-stale-tri-state-semantics.md): API `filter[stale]` tri-state semantics
- [ADR-0255](../adr/0255-org-container-keys-refresh-deletes-stale.md): org_container_keys refresh deletes stale keys
- [ADR-0132](../adr/0132-retention-policies-per-table.md): Retention policies (30-day stale cleanup)

## Koku-side stale source detection (SaaS)

Koku (sibling repository) provides **SaaS operational tooling** that complements
ROS recommendation staleness. ROS marks workloads stale when metrics stop
arriving; Koku detects when an OCP **source** has not completed a manifest
upload in time and can notify console.redhat.com customers via the platform
notification service.

### What it does

| Item | Detail |
|------|--------|
| Celery task | `masu.celery.tasks.check_for_stale_ocp_source` |
| Threshold | No manifest upload within **3 days** (`DateHelper.n_days_ago(..., 3)`) |
| Notification | Kafka event type `cm-operator-stale` via `NotificationService.ocp_stale_source_notification()` |
| Manual trigger | Masu API: `GET /api/cost-management/v1/notifications/?stale_ocp_check` (optional `provider_uuid`) |

Koku implementation: `koku/masu/celery/tasks.py`, `koku/masu/api/notifications.py`,
`koku/koku/notifications.py`. See also Koku
[celery-tasks.md](https://github.com/project-koku/koku/blob/main/docs/architecture/celery-tasks.md)
(`check_for_stale_ocp_source`).

### Availability

| Deployment | Behavior |
|------------|----------|
| **SaaS (console.redhat.com)** | Supported. Can be scheduled with Celery Beat for proactive alerting. **Not** on a periodic schedule today — typically run on demand (Masu API or ops automation). |
| **On-prem** | Not used. No platform notification consumer. Equivalent signals: operator CRD `last_successful_upload_time`, ROS staleness (notification code **2**, 48h default), and recommendations API filters. |

### Why on-prem does not automate this

- On-prem has no Red Hat platform notification service to deliver `cm-operator-stale` events.
- ROS already surfaces stale clusters in the recommendations API (code **2**) where users review guidance.
- Cluster admins see upload health on the metrics operator CRD status.

### Enable periodic checks on SaaS

Add to Koku Celery Beat configuration (example — daily 06:00 UTC):

```python
"check-stale-ocp-sources": {
    "task": "masu.celery.tasks.check_for_stale_ocp_source",
    "schedule": crontab(hour=6, minute=0),
},
```

### `hccm_count_stale_providers` metric

Legacy Grafana dashboards reference `hccm_count_stale_providers`; **no producer
exists** in the Koku codebase. For operator/source upload observability, use
Koku `GET /sources/` and `last_payload_received_at` instead. Low priority —
ROS staleness (this document) is the user-facing signal in Optimizations.
