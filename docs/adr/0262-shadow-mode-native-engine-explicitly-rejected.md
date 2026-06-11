# ADR-0262: Shadow-mode native engine explicitly rejected

## Status

Accepted

## Phase

1–2

## Context

Safe rollout of the native engine could use shadow tables (`recommendation_sets_shadow`) to write native results alongside Kruize without affecting production reads. This was designed during phase 1 planning but rejected before implementation.

Gradual traffic shift between Kruize and native would require dual-read paths and complex routing logic.

## Decision

Reject shadow tables in favor of: (1) comparison CLI tool ([ADR-0140](0140-kruize-native-comparison-cli.md)) for offline validation, (2) plugin exclusivity ([ADR-0104](0104-kruize-mutually-exclusive-native.md)) for atomic switchover, (3) `ROS_ENABLED_PLUGINS` for per-deployment control.

No dual-write to production tables.

## Alternatives Considered

### Shadow tables + gradual traffic shift

Complex dual-read logic and schema bloat; rejected.

### Feature flag per-org with dual-write

Still requires dual-write infrastructure without offline validation benefits.

## Consequences

- Switchover is atomic (plugin toggle), not gradual.
- Comparison must be done offline with the CLI tool.
- No A/B testing of native vs Kruize in production.
- Simpler schema (no shadow tables, no dual-read paths).

## Related Decisions

- [ADR-0104](0104-kruize-mutually-exclusive-native.md): Kruize mutually exclusive with native.
- [ADR-0140](0140-kruize-native-comparison-cli.md): Comparison CLI tool.
- [ADR-0001](0001-native-engine-over-kruize.md): Native engine over Kruize.
- [ADR-0259](0259-synchronous-ingest-time-engine-replaces-kruize-experiment-lifecycle.md): Synchronous ingest engine.

## References

- [cmd/compare-recommendations/main.go](../../cmd/compare-recommendations/main.go)
- [internal/plugins/registry.go](../../internal/plugins/registry.go)
