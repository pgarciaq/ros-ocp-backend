# ADR-0042: Use desired_replicas over pod_count_avg for savings multiplication

## Status

Accepted

## Context

Derived averages are noisy; kube-state `desired_replicas` is authoritative for scaling.

## Decision

Multiply per-pod savings by `desired_replicas` from latest operator data.

## Consequences

Accurate fleet savings. Depends on operator providing this field. Falls back to 1.

## References

- [internal/engine/savings.go](internal/engine/savings.go)
