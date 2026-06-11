# ADR-0181: Adoption detection marks all term/engine rows and emits code 6

## Status

Accepted

## Context

When a user adopts a recommendation—changing requests to match what was recommended—the system should acknowledge adoption and stop recommending the same change. Adoption must be detected with tolerance for floating-point resource values ([ADR-0037](0037-adoption-detection-5-percent-tolerance.md)) and reflected consistently across all persisted recommendation rows for a container.

## Decision

- `FindAdoptedContainers` compares current requests to prior cost-engine short-term recommendations using 5% tolerance.
- `MarkAdopted` sets `recommendation_applied_at` and appends `NotifRecApplied` (code 6) across **all** term/engine rows for that container, not only the matching term.
- Adoption detection triggers from cost-engine comparison only; quality adoption uses the same 5% rule per engine independently.

## Alternatives Considered

### Per-engine adoption only

Inconsistent user experience—some rows show applied, others continue recommending the same target.

### Exact match on requests

Too strict for millicore and byte rounding in Kubernetes resource quantities.

## Consequences

- Adopted recommendations stop accumulating savings in fleet counts.
- All term/engine rows carry code 6 after adoption, simplifying list filters.
- Quality stability and adoption are evaluated per engine ([ADR-0179](0179-recommendation-quality-stability-formula.md)).

## Related Decisions

- [ADR-0037](0037-adoption-detection-5-percent-tolerance.md): 5% adoption tolerance.
- [ADR-0179](0179-recommendation-quality-stability-formula.md): Quality stability scoring.

## References

- [internal/engine/adoption.go](../../internal/engine/adoption.go)
- [internal/engine/quality.go](../../internal/engine/quality.go)
