# ADR-0075: Use gzip for responses >1KB

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Context

Always-on compression wastes CPU on tiny health/status responses.

## Decision

Gzip middleware with 1KB threshold.

## Consequences

Bandwidth savings on list responses. No overhead on small responses.

## References

- [internal/api/server.go](internal/api/server.go)
