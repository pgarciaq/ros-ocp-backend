# ADR-0133: Use structured logging with org_id, cluster_uuid, request_id

## Status

Accepted

## Context

Printf-only logs can't be correlated across services.

## Decision

Structured JSON logging with zerolog; standard fields on every entry.

## Consequences

Machine-parseable. Correlatable. Slightly more verbose. Required for production debugging.

## References

- [internal/logging/](internal/logging/)
