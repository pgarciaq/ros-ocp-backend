# ADR-0247: APIProviders() bypasses Kruize exclusivity for namespace routes

## Status

Accepted

## Context

[ADR-0104](0104-kruize-mutually-exclusive-native.md) enforces mutual exclusivity: Kruize OR native engine per CSV ingest path and recommendation provider — not both for the same workload data.

[ADR-0163](0163-deprecate-kruize-plugin.md) deprecates Kruize, but deployments may still run Kruize mode during migration. Native namespace recommendation endpoints remain valuable for UI parity even when container CSV ingest uses Kruize.

## Decision

Plugin **exclusivity applies only** to traits that compete for the same data pipeline:

- `CSVIngestor`
- `RecommendationProvider` (produce path)

`APIProviders()` returns **all** plugins that implement API route registration, **regardless of Kruize/native exclusivity**. Native namespace list/detail routes stay mounted when Kruize mode is active for container ingest.

Kruize does not register overlapping namespace native routes; exclusivity prevents duplicate produce, not duplicate read APIs from complementary plugins.

## Alternatives Considered

### Hide native namespace API when Kruize enabled

Breaks UI features that depend on native namespace aggregates during migration.

### Single merged API provider facade

Over-abstraction; trait split already models responsibilities ([ADR-0100](0100-trait-interfaces-for-plugins.md)).

### Exclusivity on all plugin traits

Would disable GPU/VM API enrichers when Kruize runs — incorrect.

## Consequences

- OpenAPI `x-plugin-required` filtering ([ADR-0073](0073-dynamic-openapi-x-plugin-required.md)) must account for routes visible in Kruize mode.
- Operators may see native namespace data alongside Kruize container data — documented as transitional.
- New plugins implementing `APIProvider` must not assume exclusivity gating.

## Related Decisions

- [ADR-0104](0104-kruize-mutually-exclusive-native.md): Kruize mutually exclusive with native.
- [ADR-0163](0163-deprecate-kruize-plugin.md): Deprecate Kruize plugin.
- [ADR-0100](0100-trait-interfaces-for-plugins.md): Trait interfaces for plugins.

## References

- [internal/plugins/registry.go](../../internal/plugins/registry.go)
- [internal/api/routes.go](../../internal/api/routes.go)
