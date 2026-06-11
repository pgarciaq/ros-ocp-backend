# ADR-0276: HPA/VPA recommendations deferred — advisory automation model only

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Phase

7+

## Context

VPA provides pod-level vertical recommendations. HPA manages horizontal scaling. Both exist in-cluster. ROS could consume their outputs as sizing input or produce VPA/HPA objects directly.

Cluster write access and safety concerns make automated VPA object creation risky.

## Decision

Do not consume VPA/HPA recommender output as sizing input. Do not produce VPA/HPA objects. ROS provides advisory container-level recommendations; external automation (scripts, GitOps) consumes ROS API to create VPA objects if desired.

Future HPA/VPA plugins documented but not implemented.

## Alternatives Considered

### Consume VPA output as input

Couples to VPA availability and algorithm quality.

### Produce VPA objects directly

Requires cluster write access; safety concerns.

### HPA-aware sizing

Requires workload behavior modeling beyond current scope.

## Consequences

- ROS is advisory-only (no cluster mutations).
- Users must build automation to apply recommendations.
- No conflict with existing VPA installations.
- Clear separation: ROS recommends, operators act.

## Related Decisions

- [ADR-0260](0260-per-container-recommendation-granularity-operator-csv-grain.md): Per-container granularity.
- [ADR-0001](0001-native-engine-over-kruize.md): Native engine over Kruize.
- [ADR-0277](0277-local-hybrid-on-cluster-engine-deferred-central-only-v1.md): Local mode deferred.

## References

- [docs/features/vpa-hpa-integration.md](../../docs/features/vpa-hpa-integration.md)
- [internal/engine/container.go](../../internal/engine/container.go)
