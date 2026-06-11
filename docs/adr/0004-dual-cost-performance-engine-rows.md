# ADR-0004: Use dual cost/performance engine rows per term

## Status

Accepted

## Context

UI and FinOps users need both conservative (cost) and headroom (performance) sizing from the same telemetry.

## Decision

Every term-based plugin emits two rows per entity: `engine=cost` and `engine=performance` with different percentile/target configurations.

## Alternatives Considered

### Single row with nested JSON cost/performance objects
Storing both engine profiles in one JSONB column per entity would halve row count, but list queries cannot `GROUP BY` or filter on nested keys efficiently, and savings rollups in `internal/model/recommendation_set_native.go` rely on typed columns indexed per engine.

### Client-side engine toggle (one row, UI selects profile)
Serving a single conservative row and deriving performance sizing client-side would avoid duplicate storage, but doubles API round-trips when users switch engines and pushes percentile math out of the tested Go engine into koku-ui.

### UNION ALL at query time from one canonical row
Generating cost vs performance perspectives with runtime SQL unions keeps storage lean, but pagination over `org_container_keys` becomes unstable (two logical rows per entity) and breaks keyset cursors that assume one row per `(org, container, term)`.

## Consequences

Doubles recommendation row count. Enables single-ingest dual-perspective UX. PK includes `(term, engine)`.

## References

- [docs/architecture/recommendation-engines.md](docs/architecture/recommendation-engines.md)
- [internal/engine/types.go](internal/engine/types.go)
