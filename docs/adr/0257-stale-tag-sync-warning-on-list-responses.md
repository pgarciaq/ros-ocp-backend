# ADR-0257: Stale tag sync warning on list responses

## Status

Accepted

## Context

Tag filters depend on `org_tag_sync_metadata.last_synced_at` updated by full-replace sync ([ADR-0226](0226-tag-sync-full-replace-per-org.md)). When `ROS_TAGS_ENABLED` is false, filters silently no-op ([ADR-0227](0227-ros-tags-enabled-master-gate-silently-disables-tag-filters.md)) — a different failure mode.

When tags are enabled but sync lags (Koku outage, push failures, on-prem DB replication delay), filtered list results may be **incomplete or empty** without obvious cause. Operators misattribute empty lists to RBAC or stale recommendations.

## Decision

When **tag filters are active** on a list request and `org_tag_sync_metadata.last_synced_at` is older than a configurable threshold, include a **`warnings`** array entry in the JSON response (alongside `data` and `meta`):

- Code/message indicating stale tag sync
- Optional `last_synced_at` timestamp for operator visibility

Does not alter HTTP status (200 with warning). Does not block the query — results reflect current ROS tag snapshot, which may be stale relative to Koku.

## Alternatives Considered

### Fail request with 503 when sync stale

Too aggressive for transient Koku blips.

### Log only, no API surface

Operators without log access cannot self-serve.

### Auto-trigger sync on stale read

Risk of sync storms on every list call; sync remains async push/pull path.

## Consequences

- UI may surface yellow banner when `warnings` present.
- Threshold env var documented in `configuration.md` ([ADR-0238](0238-environment-variable-catalog-by-subsystem.md)).
- No warning when tag filters omitted — avoids noise on unfiltered lists.

## Related Decisions

- [ADR-0226](0226-tag-sync-full-replace-per-org.md): Tag sync full-replace per org.
- [ADR-0227](0227-ros-tags-enabled-master-gate-silently-disables-tag-filters.md): Master gate disables filters.
- [ADR-0256](0256-dual-tag-filter-syntax-legacy-koku.md): Tag filter syntax.

## References

- [internal/api/tag_filters.go](../../internal/api/tag_filters.go)
- [internal/tags/sync_metadata.go](../../internal/tags/sync_metadata.go)
