# ADR-0277: Local/hybrid on-cluster engine deferred — central processing only for v1

## Status

Accepted

## Phase

7+

## Context

Some environments need recommendations computed on-cluster (air-gapped, latency-sensitive). A local mode or hybrid mode was designed where the engine runs alongside the operator on worker nodes.

Central mode covers approximately 95% of use cases; local mode requires solving state sync, update distribution, and resource constraints.

## Decision

Defer local/hybrid modes. V1 is central-processing only: operator → Kafka → central ROS backend → API. Rationale: central mode covers majority use cases; local mode complexity not justified for v1.

## Alternatives Considered

### Local-first architecture

Complex state management on every cluster.

### Hybrid with sync

Eventual consistency challenges between central and edge.

## Consequences

- Air-gapped environments cannot use ROS without network path to central service.
- Future local mode documented in `docs/features/local-mode.md`.
- Architecture decisions (single binary, Kafka dependency) do not preclude future local mode.

## Related Decisions

- [ADR-0129](0129-multi-mode-cobra-binary.md): Multi-mode Cobra binary.
- [ADR-0088](0088-kafka-s3-pipeline-both-modes.md): Kafka pipeline.
- [ADR-0276](0276-hpa-vpa-recommendations-deferred-advisory-only.md): HPA/VPA deferred.

## References

- [docs/features/local-mode.md](../../docs/features/local-mode.md)
- [cmd/rosocp/main.go](../../cmd/rosocp/main.go)
