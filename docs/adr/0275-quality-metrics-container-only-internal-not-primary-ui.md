# ADR-0275: Quality metrics are container-only and internal (not primary UI surface)

## Status

Accepted

## Phase

4–5

## Context

Quality/stability scoring was designed for internal adoption detection and operator diagnostics. Exposing prominently in UI would require explaining statistical concepts (stability %, variance bands) to non-technical users.

Namespace, node, and GPU plugins have different signal characteristics unsuitable for the same stability formula.

## Decision

Quality metrics (stability %, adoption detection) are container-level only (not namespace/node/GPU). API endpoint exists but is not featured in primary UI navigation. Short-term cost engine row is the stability baseline. Internal use: adoption auto-detection, support diagnostics.

## Alternatives Considered

### Full UI integration

UX complexity for unclear user value.

### Remove quality entirely

Loses adoption detection capability.

## Consequences

- UI teams may request quality integration later (API ready).
- Quality not computed for non-container plugins.
- Stability formula uses single baseline (not cross-term comparison).

## Related Decisions

- [ADR-0179](0179-stability-score-formula.md): Stability score formula.
- [ADR-0181](0181-adoption-detection-auto-marking.md): Adoption auto-detection.
- [ADR-0271](0271-recommendation-history-boxplots-deferred-phase4-to-phase5.md): Phase 4–5 scope.

## References

- [internal/api/handlers_quality.go](../../internal/api/handlers_quality.go)
- [internal/engine/quality.go](../../internal/engine/quality.go)
