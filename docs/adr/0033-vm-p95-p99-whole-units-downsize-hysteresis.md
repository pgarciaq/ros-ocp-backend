# ADR-0033: Use VM P95/P99 + whole vCPU/GiB sizing with downsize hysteresis

## Status

Accepted

## Context

VMs use whole-number vCPU/GiB allocations unlike fractional container cores.

## Decision

P95 CPU, P99 memory; round up to whole units; require sustained underutilization before recommending downsize.

## Consequences

VM recommendations in whole units. Hysteresis prevents flapping. More conservative than container sizing.

## References

- [internal/engine/vm_recommender.go](internal/engine/vm_recommender.go)
