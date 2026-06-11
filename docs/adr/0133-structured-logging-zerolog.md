# ADR-0133: Use structured logging with logrus (org_id, cluster_uuid, request_id)

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Context

Printf-only logs can't be correlated across services.

## Decision

Structured JSON logging via **logrus** with JSON formatter in `internal/logging/logging.go`. Every entry carries standard correlation fields: `org_id`, `cluster_uuid`, and `request_id`.

logrus was chosen over zerolog for compatibility with `platform-go-middlewares`, which also uses logrus and provides CloudWatch integration helpers used by ROS-OCP. Standard field helpers (`ForOrg`, request-scoped entries) wrap logrus entries.

## Consequences

Machine-parseable. Correlatable. Slightly more verbose. Required for production debugging.

## References

- [internal/logging/logging.go](../../internal/logging/logging.go)
