# ADR-0125: Use single-flight lock + trailing reship on concurrent schedule edits

## Status

Accepted

## Context

Parallel reships from rapid schedule changes duplicate Kafka load.

## Decision

Single-flight lock; trailing pass after in-flight completes.

## Consequences

At most one reship in-flight. Latest schedule always applied. No redundant work.

## References

- [internal/reship/service.go](internal/reship/service.go)
