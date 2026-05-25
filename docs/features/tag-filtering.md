# Tag Filtering

Comprehensive reference for OpenShift tag synchronization from Koku to ROS and
tag-based recommendation list filtering.

---

## Overview

**Tag filtering** lets operators filter ROS recommendations by OpenShift labels
(tags) that Cost Management already tracks for billing. Tags originate on pods and
namespaces in the cluster, flow through the koku-metrics-operator into Koku
summaries, and are **pushed** to ROS so list APIs can filter by
`?filter[tag:<key>]=value1,value2`.

**Why push-based sync?**

- ROS maintains its own PostgreSQL database separate from Koku tenant schemas.
- List queries must filter on `org_container_keys.resolved_tags` without cross-service
  SQL joins at request time.
- Koku already owns tag enable/disable settings and namespace label resolution from
  OCP line items — it is the source of truth for which keys and values are valid.

Tag filtering is gated by `ROS_TAGS_ENABLED` on both Koku and ROS.

---

## Architecture

```
OpenShift cluster
  │  pod/namespace labels (Prometheus → operator CSVs)
  ▼
Koku ingestion → OCPUsageLineItemDailySummary.all_labels
  │  EnabledTagKeys (Settings API)
  ▼
Koku Celery: sync_ros_ocp_tags
  │  POST /internal/tags/sync  (Bearer SA token)
  ▼
ROS org_container_keys.resolved_tags  +  org_tag_sync_metadata
  │
  ▼
GET /recommendations/openshift?filter[tag:environment]=production
```

| Component | Location |
|-----------|----------|
| Payload builder | [`koku/masu/processor/ros_tag_sync.py`](../../../koku/koku/masu/processor/ros_tag_sync.py) |
| Sync service | [`internal/tags/sync.go`](../../internal/tags/sync.go) |
| Auth | [`internal/tags/auth.go`](../../internal/tags/auth.go) |
| HTTP handlers | [`internal/api/handlers_tags_sync.go`](../../internal/api/handlers_tags_sync.go), [`handlers_tags_status.go`](../../internal/api/handlers_tags_status.go) |
| List filter parsing | [`internal/api/utils.go`](../../internal/api/utils.go) (`parseTagFiltersFromRequest`) |

---

## Authentication

### Current: Kubernetes ServiceAccount TokenReview

Koku worker/listener pods call ROS with:

```
Authorization: Bearer <projected service-account token>
```

ROS validates via the in-cluster **TokenReview API**:

1. ROS reads its own pod ServiceAccount token (reviewer identity).
2. ROS POSTs `TokenReview` to `https://kubernetes.default.svc/apis/authentication.k8s.io/v1/tokenreviews`.
3. The API confirms the caller token is authenticated and returns the ServiceAccount username (`system:serviceaccount:<ns>:<name>`).
4. Optionally, `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` restricts which SA names are accepted (empty = any authenticated SA).

**Dev mode:** When `ROS_TAGS_DEV_TOKEN` is set on ROS, matching bearer tokens are accepted with a warning log. Koku uses the same value via `ROS_TAGS_DEV_TOKEN` when the projected SA token path is unavailable (local docker-compose).

See [tag-sync-auth.md](../operations/tag-sync-auth.md) for failure modes and monitoring.

### Future: mTLS

Planned for on-prem hardening:

- **cert-manager** — per-service client/server certificates from a cluster CA.
- **Service mesh sidecar** — Istio/Linkerd mutual TLS without application changes.

mTLS provides bidirectional transport authentication and reduces reliance on token
rotation. TokenReview will remain supported during migration behind a feature flag
(e.g. `ROS_TAGS_MTLS_ENABLED`).

---

## Sync Triggers (Event-Driven)

| # | Trigger | When | Scope |
|---|---------|------|-------|
| 1 | Tag settings API mutations | Enable/disable tag key, mapping change | Single tenant (`schedule_ros_tag_sync`) |
| 2 | OCP summarization complete | After summary tables updated for a provider | Single tenant |
| 3 | Periodic safety-net | Celery beat every **6 hours** (`sync_ros_ocp_tags_periodic`) | All tenants |

The periodic task exists to recover from transient network failures or missed
event hooks. It does **not** replace event-driven sync for low-latency settings changes.

---

## Sync Payload Format

```json
{
  "org_id": "1234567",
  "synced_at": "2026-05-25T18:00:00Z",
  "tag_keys": [
    {"key": "environment", "values": ["production", "staging"]},
    {"key": "team", "values": []}
  ],
  "namespace_tags": [
    {
      "cluster_uuid": "550e8400-e29b-41d4-a716-446655440000",
      "namespace": "payments",
      "tags": {"environment": "production", "team": "platform"}
    }
  ]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `org_id` | string | yes | Bare org ID (not `org1234567` schema prefix) |
| `synced_at` | ISO-8601 UTC | yes | When Koku built the payload |
| `tag_keys` | array | yes | All **enabled** OCP tag keys and distinct values observed in the latest billing period |
| `namespace_tags` | array | yes | Per `(cluster_uuid, namespace)` resolved tag map applied to all containers in that namespace |

**Value catalog rules:**

- `tag_keys` lists every enabled key even when `values` is empty (key enabled but not observed on any pod).
- Disabled keys are omitted entirely.
- Values are collected from `OCPUsageLineItemDailySummary.all_labels` for the latest `usage_start` day with pod data.

---

## Full-Replace Semantics

Each sync is an **org-scoped full replace** inside a single transaction:

1. `UPDATE org_container_keys SET resolved_tags = '{}'` for the org.
2. For each `namespace_tags` entry, `UPDATE org_container_keys SET resolved_tags = …` where `(org_id, cluster_uuid, namespace)` match.
3. Upsert `org_tag_sync_metadata` with `synced_at` and `tag_keys` catalog.

Implications:

- Namespaces **not** in the payload end up with empty `resolved_tags`.
- Container rows inherit **namespace-level** tags (not pod-level overrides in v1).
- A successful sync always reflects Koku's current enabled-key set — no incremental merge.

---

## Storage

| Table | Column | Purpose |
|-------|--------|---------|
| `org_container_keys` | `resolved_tags` (JSONB) | Tag map per container row; populated from namespace-level sync |
| `org_tag_sync_metadata` | `synced_at`, `tag_keys` | Org-level freshness timestamp and enabled-key catalog |

Migration: [`migrations/000082_create_org_tag_sync_metadata.up.sql`](../../migrations/000082_create_org_tag_sync_metadata.up.sql)

**ROS pod restart:** Tags persist in PostgreSQL. No in-memory tag cache is lost on restart.

---

## Freshness Monitoring

```
GET /api/cost-management/v1/internal/tags/status?org_id=1234567
Authorization: Bearer <token>
```

Response:

```json
{
  "org_id": "1234567",
  "synced_at": "2026-05-25T18:00:00Z",
  "tag_keys": [
    {"key": "environment", "values": ["production", "staging"]}
  ]
}
```

Compare `synced_at` against last OCP summarization or settings change. Alert if
stale beyond expected window (e.g. > 7 hours when periodic safety-net is 6h).

---

## API Filtering Syntax

Container list endpoints accept Koku bracket notation only:

```
GET /api/cost-management/v1/recommendations/openshift
  ?filter[tag:environment]=production,staging
  &filter[tag:team]=platform
```

| Pattern | Semantics |
|---------|-----------|
| `filter[tag:key]=value` | Exact match on resolved tag value |
| `filter[tag:key]=a,b` | OR within the same key |
| Multiple `filter[tag:*]` keys | AND across keys |
| Wildcard `*` | Not supported in v1 |

Filtering requires `ROS_TAGS_ENABLED=true` and prior successful sync for the org.

Implementation: two-step list query — step 1 resolves matching containers from
`org_container_keys.resolved_tags`; step 2 fetches recommendations for those keys.

---

## Tag Lifecycle Scenarios

### New tag key enabled in Koku

1. Operator enables key via Settings API (`enabled_tags`).
2. Koku calls `schedule_ros_tag_sync` immediately.
3. Payload includes the new key in `tag_keys` (possibly with empty `values`).
4. ROS metadata updated; filters on the new key become available (values appear after data exists).

### Tag key disabled in Koku

1. Settings mutation triggers immediate sync.
2. Key omitted from `tag_keys` and namespace maps.
3. Full-replace clears the key from all `resolved_tags` rows.
4. ROS logs removed keys (`tag sync: removed tag key …`).

### Tag mapping changed in Koku

1. Settings mutation triggers immediate sync.
2. Next payload reflects new key→label resolution from updated mappings.
3. Full-replace overwrites all namespace tag maps.

### Tag key disappears from cluster (still enabled in Koku)

The operator stops reporting the label on pods, but the key remains enabled in Settings.

1. Next OCP summarization processes line items without that label.
2. Sync sends key in `tag_keys` with **fewer or empty** `values`.
3. Namespace maps omit the key.
4. ROS retains the key in metadata catalog with empty values until disabled manually.
5. Filters using old values return zero results (correct behavior).

### New tag values appear for existing key

1. New pods/namespaces labeled or relabeled.
2. Next daily OCP summarization includes new values in `all_labels`.
3. Post-summary sync adds values to `tag_keys` and namespace maps.
4. ROS filters match the new values on next list request.

### Tag values disappear (pods deleted or relabeled)

1. Next summarization reflects only current line items.
2. Sync payload contains only current values.
3. Full-replace removes stale values from `resolved_tags`.
4. Old values no longer match filters.

### Network failure during sync

1. Koku logs `ROS tag sync failed` and Celery retries per task policy.
2. ROS retains previous `resolved_tags` until a sync succeeds.
3. Periodic 6-hour safety-net re-queues all tenants.
4. **Eventual consistency** — filters may be stale until recovery.

### ROS pod restart

No state loss. Tags live in `org_container_keys` and `org_tag_sync_metadata`.
In-flight sync requests should be retried by Koku on failure.

---

## Configuration

### ROS (ros-ocp-backend)

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_TAGS_ENABLED` | `false` | Master switch: sync endpoints + list filters |
| `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` | (empty) | Comma-separated SA names allowed to push |
| `ROS_TAGS_DEV_TOKEN` | (empty) | Dev-only static bearer token |

### Koku

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_TAGS_ENABLED` | `false` | Enables sync Celery tasks |
| `ROS_OCP_BACKEND_URL` | `http://cost-onprem-ros-api:8000` | ROS API base URL |
| `ROS_TAGS_DEV_TOKEN` | (empty) | Dev bearer token when SA token unavailable |

---

## Scalability Considerations

| Dimension | Approach |
|-----------|----------|
| Thousands of orgs | Periodic task fans out one Celery task per tenant; no single giant payload |
| ~200 enabled tags per org | `tag_keys` catalog is JSONB metadata; list filter uses indexed container key lookup |
| Large namespace counts | Sync updates by `(org_id, cluster_uuid, namespace)` batch in one transaction |
| List API | Tag filter narrows container keys **before** recommendation join (see query-performance doc) |

Monitor sync duration and `updated` row counts in Koku logs. Consider alerting on
repeated sync failures per org.

---

## Future Enhancements

| Enhancement | Description |
|-------------|-------------|
| mTLS authentication | cert-manager or mesh sidecar; see [tag-sync-auth.md](../operations/tag-sync-auth.md) |
| `group_by[tag:key]=*` | Aggregate recommendations by tag dimension in API responses |
| Tag-based cost allocation | Correlate ROS savings with Koku tag breakdown reports |
| Webhook instant sync | Replace or supplement 6-hour safety-net with push notifications |
| Tag value autocomplete API | UI typeahead from `org_tag_sync_metadata.tag_keys` |
| Cross-provider tag unification | Align AWS/Azure/GCP tag keys with OCP for hybrid dashboards |
| Pod-level tag overrides | Support pod labels distinct from namespace defaults |

---

## Related Documentation

- [Tag sync operations](../operations/tag-sync.md)
- [Tag sync authentication](../operations/tag-sync-auth.md)
- [API query parameters](../operations/api-query-parameters.md)
- [Koku ROS integration](../../../koku/docs/architecture/ros-ocp-integration.md)
