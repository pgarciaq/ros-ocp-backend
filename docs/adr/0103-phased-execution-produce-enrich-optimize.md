# ADR-0103: Use phased execution (Produce/Enrich/Optimize) with priority ordering

## Status

Accepted

## Context

Cross-plugin ordering matters (namespace after container; quota after namespace).

## Decision

Three phases with priority within each; barriers between phases.

## Alternatives Considered

### Explicit dependency DAG between plugins
A full directed acyclic graph with per-plugin dependency edges would handle arbitrary plugin counts, but ROS has exactly three phases (Produce → Enrich → Optimize) with stable ordering rules—a DAG engine adds parsing, cycle detection, and debugging surface for no gain over `phases.go`.

### Topological sort of plugins at runtime
Sorting plugins by declared dependencies each ingest cycle adapts to dynamic registration, but a misconfigured dependency creates fragile startup failures and non-deterministic ordering when priorities tie—explicit phase assignment is auditable in code review.

### Fully parallel plugin execution within phases
Running all container/namespace/quota plugins concurrently minimizes wall time, but shared mutable state (namespace aggregates consumed by quota plugins, GPU enricher reading container outputs) produces race conditions without fine-grained locking per org.

## Consequences

Correct dependency ordering. Phases can't parallelize internally. Clear execution model.

## References

- [internal/plugin/phases.go](internal/plugin/phases.go)
