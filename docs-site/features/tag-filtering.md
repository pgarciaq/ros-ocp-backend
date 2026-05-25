# Tag Filtering

ROS recommendation list APIs support OpenShift tag filters:

```
?filter[tag:environment]=production,staging
```

Multiple tag keys **AND** together; comma-separated values **OR** within a key.

## On-prem (default): direct database reads

When `ROS_TAGS_ENABLED=true` and `ROS_TAGS_SOURCE=db` (default), ROS reads Koku tag
data from the **same PostgreSQL** instance at query time:

| Koku table | Schema | Used for |
|------------|--------|----------|
| `reporting_enabledtagkeys` | `org{org_id}` | Which tag keys are enabled for OCP |
| `reporting_ocptags_values` | `org{org_id}` | Distinct key/value pairs with cluster and namespace arrays |

No HTTP push, Celery sync, or ServiceAccount auth is required. Tags are always fresh
after Koku completes OCP summarization.

List queries join `org_container_keys` to `reporting_ocptags_values` on
`(cluster_uuid, namespace)`.

## SaaS fallback: push sync

When `ROS_TAGS_SOURCE=api`, Koku pushes namespace-level tags into
`org_container_keys.resolved_tags` via `POST /internal/tags/sync`. List filters use
that JSONB column (same bracket syntax).

See also:

- Full reference: [`docs/features/tag-filtering.md`](../docs/features/tag-filtering.md)
- Configuration: [Configuration](configuration.md#tag-sync)
- Push auth (api only): [`docs/operations/tag-sync-auth.md`](../docs/operations/tag-sync-auth.md)
