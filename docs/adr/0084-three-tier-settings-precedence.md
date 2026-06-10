# ADR-0084: Use three-tier settings precedence: env lock → DB → default

## Status

Accepted

## Context

DB-only overrides that SaaS ops couldn't enforce globally.

## Decision

Environment variable locks override tenant DB settings which override compiled defaults.

## Consequences

Ops can force-lock settings. Tenants can customize within bounds. Clear precedence chain.

## References

- [docs/architecture/configurability.md](docs/architecture/configurability.md)
- [internal/engine/threshold_settings.go](internal/engine/threshold_settings.go)
