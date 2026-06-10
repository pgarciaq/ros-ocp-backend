# ADR-0008: Use 25 millicore CPU floor

## Status

Accepted

## Context

Zero or very small CPU recommendations would be rejected by Kubernetes or unusably small.

## Decision

Floor all CPU recommendations at 25m (millicores).

## Consequences

Prevents invalid/unusable recommendations. May slightly over-provision truly idle containers.

## References

- [internal/engine/types.go](internal/engine/types.go)
