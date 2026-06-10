# ADR-0083: Use capabilities endpoint listing locked settings fields

## Status

Accepted

## Context

Env-only locks invisible to UI admins.

## Decision

`/capabilities` endpoint exposes which settings fields are admin-locked.

## Consequences

UI can disable locked fields. Self-documenting configuration state.

## References

- [docs/architecture/configurability.md](docs/architecture/configurability.md)
