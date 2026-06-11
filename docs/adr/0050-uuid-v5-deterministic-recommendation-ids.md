# ADR-0050: Use UUID v5 deterministic recommendation IDs

## Status

Accepted

## Context

UI deep links need stable IDs; random UUIDs change on every ingest.

## Decision

Derive recommendation ID via UUID v5 from composite key fields.

## Alternatives Considered

### Random UUID v4 per ingest
Random IDs are trivial to generate and guaranteed unique, but change on every re-ingest of the same workload—breaking UI deep links, bookmarked recommendation URLs, and adoption-tracking history that keys off stable identifiers.

### Auto-increment serial primary key
Database sequences are simple and index-friendly, but expose enumeration attacks (incrementing IDs leak recommendation counts) and require a DB round-trip before the UI can construct a link during ingest.

### Composite natural key in URL path
Using `(cluster_uuid, namespace, container, term, engine)` directly in API paths avoids hashing, but produces unwieldy URLs with encoding issues for special characters and exceeds path length limits for long OpenShift resource names.

## Consequences

Stable URLs. Deterministic. Must include org_id check to prevent IDOR.

## References

- [docs/architecture/recommendation-ids.md](docs/architecture/recommendation-ids.md)
