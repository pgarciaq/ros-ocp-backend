# ADR-0080: Use CSV export via format=csv on list endpoints

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Context

Separate export microservice adds operational complexity for analyst workflows.

## Decision

Same list endpoints support `format=csv` query parameter for streaming CSV download.

## Consequences

No extra service. Reuses existing auth/filtering. Streaming prevents OOM on large exports.

## References

- [internal/api/handlers](internal/api/handlers)
