# ADR-0105: Keep container handlers in core; plugins register domain routes

## Status

Accepted

## Context

Moving container HTTP entirely into plugin would break Kruize fallback during migration.

## Decision

Container API handlers remain in core; plugins register additional routes via APIProvider trait.

## Consequences

Kruize fallback preserved. Container is always available. Plugins extend, not replace.

## References

- [internal/api/server.go](internal/api/server.go)
