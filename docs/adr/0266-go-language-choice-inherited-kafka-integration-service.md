# ADR-0266: Go language choice — inherited from pre-existing Kafka integration service

## Status

Accepted

## Phase

Pre-0

## Context

ros-ocp-backend existed as a Go service before the native engine work began. It handled Kafka consumption, Kruize API calls, and REST API serving. The native engine was added to this existing codebase rather than starting fresh.

Team had Go experience from the existing service; rewrite cost was prohibitive for phase 0 delivery.

## Decision

Continue in Go rather than rewrite. Rationale: existing Kafka/API infrastructure, team familiarity, performance adequate for compute workloads, strong concurrency primitives for parallel ingest.

## Alternatives Considered

### Java (Kruize sidecar ecosystem)

JVM overhead and different team skill set.

### Rust

Rewrite cost and team unfamiliarity.

### Python

Performance inadequate for percentile computation at ingest scale.

## Consequences

- Go type system limits some abstractions (generics added in 1.18+).
- Existing test infrastructure reusable.
- Deployment infrastructure unchanged.
- No JVM warmup latency on cold starts.

## Related Decisions

- [ADR-0001](0001-native-engine-over-kruize.md): Native engine over Kruize.
- [ADR-0267](0267-echo-framework-inherited-pre-existing-service.md): Echo framework inherited.
- [ADR-0282](0282-cgo-confluent-kafka-go-test-isolation-strategy.md): CGO Kafka dependency.

## References

- [go.mod](../../go.mod)
- [cmd/rosocp/main.go](../../cmd/rosocp/main.go)
