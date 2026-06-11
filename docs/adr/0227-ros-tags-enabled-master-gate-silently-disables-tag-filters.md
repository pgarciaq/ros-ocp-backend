# ADR-0227: ROS_TAGS_ENABLED master gate silently disables tag filters

## Status

Accepted

## Context

Tag functionality may be disabled in deployments without Koku integration — tags come from Koku cost model data. Clients may still send tag filter parameters from shared UI code.

## Decision

When `ROS_TAGS_ENABLED=false`, `TagFiltersFromParams` returns nil — tag filter query parameters are silently ignored (not rejected). API responses include all data regardless of tag filter params.

## Alternatives Considered

### 400 on tag params when disabled

Breaks clients that always send tag filters unconditionally.

### Response warning field

Adds complexity to response envelope; deferred.

## Consequences

- Filters appear accepted (no 400 error) but have no effect.
- Users may not realize tags are disabled.
- No "tags not available" warning in response (considered for future).

## Related Decisions

- [ADR-0226](0226-tag-sync-full-replace-per-org.md): Tag sync model.
- [ADR-0054](0054-resolved-tags-jsonb-on-keys-table.md): JSONB on keys table.
- [ADR-0122](0122-tags-enabled-by-default.md): Default enabled after stabilization.

## References

- [internal/tags/filters.go](../../internal/tags/filters.go)
- [internal/config/config.go](../../internal/config/config.go)
