# ADR-0039: Persist notification codes as SMALLINT[], not JSONB

## Status

Accepted

## Context

Need indexable `@>` filtering on notification codes for API queries.

## Decision

Store as PostgreSQL `SMALLINT[]` array, enabling array containment operators.

## Consequences

Indexable. Compact. GIN index support. Not as flexible as JSONB for metadata.

## References

- [docs/architecture/database-conventions.md](docs/architecture/database-conventions.md)
