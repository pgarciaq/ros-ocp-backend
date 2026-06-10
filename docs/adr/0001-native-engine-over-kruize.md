# ADR-0001: Use native Go engine over Kruize for production recommendations

## Status

Accepted

## Context

Kruize was an external Java service with JSONB-heavy storage, limited notification/savings coverage, and tight coupling via HTTP + Kafka recommendation topics.

## Decision

Build an in-process Go engine that writes relational columns; Kruize becomes optional legacy plugin (`ROS_ENABLED_PLUGINS=kruize`), mutually exclusive with native plugins.

## Consequences

Eliminated Java dependency, JSONB overhead, HTTP latency. Requires maintaining engine math in-house. Kruize path retained for rollback.

## References

- [docs/architecture/native-migration.md](docs/architecture/native-migration.md)
- [internal/engine/recommend_all.go](internal/engine/recommend_all.go)
