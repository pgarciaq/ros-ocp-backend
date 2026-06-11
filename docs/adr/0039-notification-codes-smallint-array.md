# ADR-0039: Persist notification codes as SMALLINT[], not JSONB

## Status

Accepted

## Context

Need indexable `@>` filtering on notification codes for API queries.

## Decision

Store as PostgreSQL `SMALLINT[]` array, enabling array containment and overlap operators (`@>`, `&&`).

## Alternatives Considered

### JSONB array
Weaker GIN index efficiency for numeric overlap filters; `@>` on JSONB arrays is slower than native `SMALLINT[]` overlap at list-query scale.

### Comma-separated TEXT
Cannot use PostgreSQL `&&` overlap operator; requires string parsing or `LIKE` patterns that defeat indexing.

### Separate row per notification code
Row explosion for containers with many codes; pagination and savings rollups double-count entities.

## Consequences

Indexable. Compact. GIN index support. Not as flexible as JSONB for metadata.

## References

- [docs/architecture/database-conventions.md](docs/architecture/database-conventions.md)
