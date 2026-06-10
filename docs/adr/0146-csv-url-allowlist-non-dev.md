# ADR-0146: Require explicit CSV URL allowlist in non-dev

## Status

Accepted

## Context

Open fetch-any-URL from Kafka metadata is unsafe.

## Decision

CSV download URLs must match configured allowlist domains/prefixes.

## Consequences

Only known S3 endpoints allowed. Must configure allowlist per deployment.

## References

- [internal/utils/csv_security.go](internal/utils/csv_security.go)
