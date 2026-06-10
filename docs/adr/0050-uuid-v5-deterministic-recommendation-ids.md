# ADR-0050: Use UUID v5 deterministic recommendation IDs

## Status

Accepted

## Context

UI deep links need stable IDs; random UUIDs change on every ingest.

## Decision

Derive recommendation ID via UUID v5 from composite key fields.

## Consequences

Stable URLs. Deterministic. Must include org_id check to prevent IDOR.

## References

- [docs/architecture/recommendation-ids.md](docs/architecture/recommendation-ids.md)
