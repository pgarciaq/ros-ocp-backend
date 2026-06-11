# ADR-0266: Go language choice for native engine integration

## Status

Accepted

## Phase

Pre-0

## Context

The native engine work replaced Kruize (Java/Autotune) as the recommendation backend. ros-ocp-backend already existed as a Go service handling Kafka consumption, Kruize API calls, and REST API serving — but the language choice was not automatic.

The team evaluated whether to extend the existing Go service with the native engine or rewrite the integration layer in Java to share Kruize's recommendation algorithms and JVM ecosystem directly.

## Decision

Continue in Go and add the native engine to the existing codebase.

Rationale:

- **Existing infrastructure** — Kafka consumer pipeline, REST API, deployment artifacts, and test harness already in Go.
- **Compute performance** — percentile aggregation and ingest-time engine runs benefit from Go's low allocation overhead versus JVM warmup and GC pauses at batch scale.
- **Concurrency** — goroutines and worker pools fit partition-scoped ingest and parallel cluster processing without heavyweight thread pools.
- **Operational cost** — avoiding a second runtime (JVM alongside Go) simplifies on-prem packaging and pod resource profiles.
- **Team alignment** — engineers on the native engine path already worked in Go; a Java rewrite would delay phase-0 delivery without reusing Kruize algorithms (the engine logic was rewritten in Go regardless).

## Alternatives Considered

### Java (extend Kruize/Autotune sidecar ecosystem)

Would allow sharing JVM libraries with Kruize, but the native engine algorithms were being rewritten in Go anyway. JVM overhead, separate deployment artifact, and a different skill mix from the integration team made this a full rewrite with limited reuse.

### Rust

Strong performance but full rewrite cost and team unfamiliarity.

### Python

Performance inadequate for percentile computation at ingest scale.

## Consequences

- Go type system limits some abstractions (generics added in 1.18+).
- Existing test infrastructure reusable.
- Deployment infrastructure unchanged.
- No JVM warmup latency on cold starts.
- Native engine and integration layer share one binary and one operational model.

## Related Decisions

- [ADR-0001](0001-native-engine-over-kruize.md): Native engine over Kruize.
- [ADR-0267](0267-echo-framework-inherited-pre-existing-service.md): Echo framework inherited.
- [ADR-0282](0282-cgo-confluent-kafka-go-test-isolation-strategy.md): CGO Kafka dependency.

## References

- [go.mod](../../go.mod)
- [cmd/rosocp/main.go](../../cmd/rosocp/main.go)
