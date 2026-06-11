# ADR-0246: Boot fatal on CSV type collision between plugins

## Status

Accepted

## Context

Each `CSVIngestor` plugin claims one or more CSV report type strings ([ADR-0095](0095-csv-type-longest-prefix-first.md)). If two enabled plugins claim the same type, ingest could route rows to the wrong processor silently — corrupting digests and recommendations.

[ADR-0245](0245-plugin-init-registration-order-undefined.md) makes import order an unreliable tie-breaker. Runtime "first registered wins" would hide misconfiguration until data corruption appears in production.

## Decision

At startup, after all plugins register, the registry verifies that enabled `CSVIngestor` plugins have **pairwise disjoint** CSV type claims. On any overlap, the process exits via **`log.Fatalf`** with both plugin names and the conflicting type.

Custom plugins must not overlap core plugin CSV types (pod usage, node labels, GPU usage, etc.). Overlap is a build/deploy configuration error, not a runtime warning.

## Alternatives Considered

### First-registered wins with warning log

Silent data routing risk; unacceptable for financial recommendations.

### Priority integer on CSV types

Adds operator-facing complexity; disjoint claims are simpler invariant.

### Runtime skip duplicate plugin

Leaves one plugin silently disabled — hard to diagnose.

## Consequences

- Enabling two plugins that ingest the same CSV file fails fast in CI and staging.
- Plugin extraction reviews must audit CSV claim lists in `_example` checklist ([ADR-0110](0110-example-plugin-trait-checklist.md)).
- Kruize exclusivity ([ADR-0104](0104-kruize-mutually-exclusive-native.md)) prevents overlapping native/Kruize paths at config level; this ADR catches custom plugin mistakes.

## Related Decisions

- [ADR-0245](0245-plugin-init-registration-order-undefined.md): Undefined init order.
- [ADR-0099](0099-compile-time-in-process-plugins.md): Plugin system.
- [ADR-0104](0104-kruize-mutually-exclusive-native.md): Kruize mutually exclusive with native.

## References

- [internal/plugins/registry.go](../../internal/plugins/registry.go)
- [internal/plugins/validate.go](../../internal/plugins/validate.go)
