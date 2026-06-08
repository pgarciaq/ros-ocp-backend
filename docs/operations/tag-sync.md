# Tag Sync (Koku → ROS)

> **Applies only when `ROS_TAGS_SOURCE=api` (SaaS).**  
> On-prem deployments with shared PostgreSQL (`ROS_TAGS_SOURCE=db`, default) do **not**
> use HTTP tag sync. ROS reads Koku tenant tables directly — see
> [tag-filtering.md](../features/tag-filtering.md#on-prem-shared-database).

When Koku and ROS use separate databases, Koku pushes enabled OpenShift tag keys and
namespace-level resolved tags to ROS after settings changes, OCP summarization, and
periodic safety-net runs.

---

## Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/api/cost-management/v1/internal/tags/sync` | Full-replace sync for one org |
| `GET` | `/api/cost-management/v1/internal/tags/status` | Per-org sync freshness (`synced_at`, tag key catalog) |

Both endpoints require bearer auth when `ROS_TAGS_SOURCE=api` — see [tag-sync-auth.md](tag-sync-auth.md).
They return **404** when `ROS_TAGS_SOURCE=db`.

---

## Sync triggers (Koku Celery)

| Task | Trigger |
|------|---------|
| `sync_ros_ocp_tags` | Tag settings mutations, OCP summarization complete |
| `sync_ros_ocp_tags_periodic` | Celery beat every 6 hours (`:15` past the hour) — all tenants |

Koku implementation: [`koku/masu/processor/ros_tag_sync.py`](../../../koku/koku/masu/processor/ros_tag_sync.py)

When `ROS_TAGS_SOURCE=db`, these tasks are no-ops.

---

## Payload semantics

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
      "cluster_uuid": "...",
      "namespace": "payments",
      "tags": {"environment": "production"}
    }
  ]
}
```

| Field | Meaning |
|-------|---------|
| `synced_at` | ISO-8601 UTC timestamp when Koku built the payload |
| `tag_keys` | All **enabled** OCP tag keys and distinct values in the current billing period |
| `namespace_tags` | Per `(cluster_uuid, namespace)` resolved tags applied to all containers in that namespace |

---

## Full-replace semantics

[`internal/tags/sync.go`](../../internal/tags/sync.go) implements org-scoped full replace:

1. Reset `org_container_keys.resolved_tags` to `{}` for the org.
2. Apply each `namespace_tags` entry to matching rows (`org_id`, `cluster_uuid`, `namespace`).
3. Upsert `org_tag_sync_metadata` with `synced_at` and `tag_keys` catalog.

Namespaces **not** in the payload end up with empty `resolved_tags` after step 1.
Disabled tag keys are omitted from `tag_keys` and from namespace tag maps.

---

## Lifecycle scenarios

| Scenario | Koku behavior | ROS result |
|----------|---------------|------------|
| Tag key disappears from pods (still enabled) | `tag_keys` includes key with fewer/empty `values`; namespace maps omit the key | Filters stop matching removed values |
| Tag disabled in Settings | Key omitted from payload; immediate sync | Full-replace clears key from all containers |
| New tag value appears | Next summarization adds value | Updated on next sync |
| Network failure / missed event | Periodic 6-hour safety-net | `synced_at` advances when sync succeeds |

For on-prem lifecycle (live DB reads), see [tag-filtering.md](../features/tag-filtering.md#tag-lifecycle-scenarios).

---

## Storage

| Table | Column | Purpose |
|-------|--------|---------|
| `org_container_keys` | `resolved_tags` | JSONB tag map per container (namespace-level) |
| `org_tag_sync_metadata` | `synced_at`, `tag_keys` | Org-level sync freshness and enabled-key catalog |

Migration: [`000082_create_org_tag_sync_metadata.up.sql`](../../migrations/000082_create_org_tag_sync_metadata.up.sql)

---

## Implementation

- Sync service: [`internal/tags/sync.go`](../../internal/tags/sync.go)
- HTTP handlers: [`internal/api/handlers_tags_sync.go`](../../internal/api/handlers_tags_sync.go)
- Koku sender: [`koku/masu/processor/ros_tag_sync.py`](../../../koku/koku/masu/processor/ros_tag_sync.py)
- Dual-path overview: [tag-filtering.md](../features/tag-filtering.md)
