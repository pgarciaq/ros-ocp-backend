# ADR-0092: Use separate ingest statement timeout (120s) via SET LOCAL

## Status

Accepted

## Context

Global 25s statement_timeout kills large batch upserts; removing it exposes API to runaway queries.

## Decision

Ingestion transactions use `SET LOCAL statement_timeout = '120s'`; API keeps 25s default.

## Consequences

Large ingests complete. API still protected. Session-scoped, no global side effects.

## References

- [internal/db/statement_timeout.go](internal/db/statement_timeout.go)
