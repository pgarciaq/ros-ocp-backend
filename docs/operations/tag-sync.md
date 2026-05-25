# Tag Sync (Koku → ROS)

Koku pushes enabled OpenShift tag keys and namespace-level resolved tags to ROS after
settings changes, OCP summarization, and periodic safety-net runs.

## Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/api/cost-management/v1/internal/tags/sync` | Full-replace sync for one org |
| `GET` | `/api/cost-management/v1/internal/tags/status` | Per-org sync freshness (`synced_at`, tag key catalog) |

Both endpoints require bearer auth — see [tag-sync-auth.md](tag-sync-auth.md).

## Payload Semantics

Koku sends:

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
| `tag_keys` | All **enabled** OCP tag keys and their distinct values in the current billing period; empty `values` when the key is enabled but not observed on any pod |
| `namespace_tags` | Per `(cluster_uuid, namespace)` resolved tags applied to all containers in that namespace |

## Full-Replace Semantics

[`internal/tags/sync.go`](../../internal/tags/sync.go) implements org-scoped full replace:

1. Reset `org_container_keys.resolved_tags` to `{}` for the org.
2. Apply each `namespace_tags` entry to matching rows (`org_id`, `cluster_uuid`, `namespace`).
3. Upsert `org_tag_sync_metadata` with `synced_at` and `tag_keys` catalog.

Namespaces **not** in the payload end up with empty `resolved_tags` after step 1.
Disabled tag keys are omitted from `tag_keys` and from namespace tag maps.

## Lifecycle Scenarios

| Scenario | Koku behavior | ROS result |
|----------|---------------|------------|
| Tag key disappears from pods (still enabled) | `tag_keys` includes key with fewer/empty `values`; namespace maps omit the key | Filters stop matching removed values; metadata reflects empty catalog entry |
| Tag disabled in Settings | Key omitted from payload; settings mutation triggers immediate sync | Full-replace clears key from all containers |
| New tag value appears | Next summarization adds value to `tag_keys` and namespace maps | Updated on next sync |
| Network failure / missed event | Periodic 6-hour safety-net sync retries | `synced_at` advances when sync succeeds |

## Storage

| Table | Column | Purpose |
|-------|--------|---------|
| `org_container_keys` | `resolved_tags` | JSONB tag map per container (namespace-level) |
| `org_tag_sync_metadata` | `synced_at`, `tag_keys` | Org-level sync freshness and enabled-key catalog |

Migration: [`000082_create_org_tag_sync_metadata.up.sql`](../../migrations/000082_create_org_tag_sync_metadata.up.sql)

## Implementation

- Sync service: [`internal/tags/sync.go`](../../internal/tags/sync.go)
- Status query: [`internal/tags/status.go`](../../internal/tags/status.go)
- HTTP handlers: [`internal/api/handlers_tags_sync.go`](../../internal/api/handlers_tags_sync.go)
- Koku sender: [`koku/masu/processor/ros_tag_sync.py`](../../../../koku/koku/masu/processor/ros_tag_sync.py)
