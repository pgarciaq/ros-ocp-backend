# ADR-0133: Use structured logging with org_id, cluster_uuid, request_id

## Status

Accepted

## Context

Printf-only logs can't be correlated across services.

## Decision

Structured JSON logging with standard fields on every entry (`org_id`, `cluster_uuid`, `request_id`).

## Implementation Note

The ADR title references zerolog as the originally considered library. The accepted implementation uses **logrus** with JSON formatter in `internal/logging/logging.go`.

Zerolog was evaluated but logrus was chosen for compatibility with `platform-go-middlewares`, which also uses logrus and provides CloudWatch integration helpers used by ROS-OCP.

Standard field helpers (`ForOrg`, request-scoped entries) wrap logrus entries; behavior matches the original decision intent (structured, correlatable logs) with a different library.

## Consequences

Machine-parseable. Correlatable. Slightly more verbose. Required for production debugging.

## References

- [internal/logging/logging.go](../../internal/logging/logging.go)
