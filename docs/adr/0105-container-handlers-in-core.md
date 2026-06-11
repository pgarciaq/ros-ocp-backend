# ADR-0105: Keep container handlers in core; plugins register domain routes

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Context

Moving container HTTP entirely into plugin would break Kruize fallback during migration.

## Decision

Container API handlers remain in core; plugins register additional routes via APIProvider trait.

## Consequences

Kruize fallback preserved. Container is always available. Plugins extend, not replace.

## References

- [internal/api/server.go](internal/api/server.go)
