# ADR-0063: Centralize migrations in one numbered directory with plugin headers

## Status

Accepted

## Context

Per-plugin migration trees would break golang-migrate's sequential ordering.

## Decision

Single `migrations/` directory; files annotated with `-- plugin: <name>` comments.

## Alternatives Considered

### Per-plugin migration directories
golang-migrate requires global sequential numbering; split directories create ordering conflicts when two plugins ship migrations in the same release.

### Runtime DDL from plugins
Risky in production—no review gate, no rollback scripts, and race conditions during rolling deploys.

### Shared migration file without plugin ownership markers
Blame and rollback scope unclear when a migration touches multiple plugin tables; on-call cannot identify owning team quickly.

## Consequences

Simple migration tooling. Clear ownership via comments. No per-plugin rollback isolation.

## References

- [migrations/README.md](migrations/README.md)
