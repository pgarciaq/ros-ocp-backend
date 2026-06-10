# ADR-0063: Centralize migrations in one numbered directory with plugin headers

## Status

Accepted

## Context

Per-plugin migration trees would break golang-migrate's sequential ordering.

## Decision

Single `migrations/` directory; files annotated with `-- plugin: <name>` comments.

## Consequences

Simple migration tooling. Clear ownership via comments. No per-plugin rollback isolation.

## References

- [migrations/README.md](migrations/README.md)
