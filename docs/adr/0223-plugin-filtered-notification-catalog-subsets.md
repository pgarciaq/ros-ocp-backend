# ADR-0223: Plugin-filtered notification catalog subsets

## Status

Accepted

## Context

Different plugins emit different notification codes. The catalog endpoint should only show codes relevant to enabled plugins — for example, a VM-only deployment omits GPU codes.

## Decision

`BuildCatalog(pluginFilter)` returns filtered code sets using `pluginCatalogCodes` mapping. Each plugin declares which notification codes it can emit. API serves the intersection of enabled plugins' codes.

## Alternatives Considered

### Always serve full catalog

Exposes codes for disabled features; confuses operators.

### Per-request plugin query parameter

Over-engineering; deployment config is sufficient.

## Consequences

- Catalog response varies by deployment configuration.
- Contract tests use full registry (all plugins enabled).
- Adding a code to a plugin requires updating `pluginCatalogCodes`.

## Related Decisions

- [ADR-0077](0077-notification-codes-catalog-endpoint.md): Catalog endpoint.
- [ADR-0222](0222-notification-dual-source-db-seed-and-go-definitions.md): Dual source catalog.
- [ADR-0168](0168-disabled-plugin-route-guards.md): Disabled plugin guards.

## References

- [internal/notifications/catalog.go](../../internal/notifications/catalog.go)
- [internal/api/handlers_notification_codes.go](../../internal/api/handlers_notification_codes.go)
