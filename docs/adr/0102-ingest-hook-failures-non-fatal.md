# ADR-0102: Treat IngestHook failures as non-fatal

## Status

Accepted

## Context

GPU digest upsert bug must not block container recommendations (primary product).

## Decision

Hook errors logged + counted but processing continues.

## Alternatives Considered

### Fail-closed on hook error
One auxiliary plugin failure (e.g. GPU digest upsert) blocks all container recommendations—the primary product value.

### Retry loop inside ingest pipeline
Delays entire ingest batch; poison-hook scenarios stall Kafka consumer lag unbounded.

## Consequences

Container reliability isolated from auxiliary plugins. Monitor `rosocp_ingest_hook_failures_total`; degradation surfaced via `ingest_hooks_failed` flag on ingest status responses.

## References

- [docs/architecture/plugin-architecture.md](docs/architecture/plugin-architecture.md)
