# ADR-0012: Use three-state idle/zombie/active classification

## Status

Accepted

## Context

Binary idle-only model couldn't distinguish deletable zombies from low-but-nonzero workloads.

## Decision

Three states based on request-relative utilization: idle (zero usage), zombie (negligible), active.

## Alternatives Considered

### Binary idle/active
Low-but-nonzero workloads lumped into "active" hide deletable zombies; true idle mixed with zombies blocks targeted cleanup actions. Rejected because operators need distinct guidance for "scale to zero" vs "delete orphaned deployment."

## Related Decisions

- [ADR-0038](0038-notification-code-bitmap-1-63.md): notification codes map idle → `NotifIdle`, zombie → `NotifZombie` for API filtering and UI badges.

## Consequences

Enables targeted UI actions (delete zombie vs right-size idle). More notification codes needed.

## References

- [internal/engine/idle_classification.go](internal/engine/idle_classification.go)
- [docs/features/idle-detection.md](docs/features/idle-detection.md)
