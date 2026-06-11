# ADR-0126: Use forward-only fallback when reship fails after max retries

## Status

Accepted

## Context

Blocking BH settings forever on historical gaps is worse than partial data.

## Decision

After max reship retries, fall forward with partial BH data—apply new settings going forward, accept historical gap.

## Alternatives Considered

### Block BH settings permanently on reship failure
Operators cannot save schedule changes; settings UI appears broken until manual intervention.

### Retry indefinitely
Resource waste on Koku listener and ROS worker; poison-payload scenarios never converge.

## Consequences

BH settings always eventually apply. Historical gaps possible—notification flags the gap. Operator can manually trigger reship from internal endpoint after fixing upstream issue.

## References

- [internal/reship/service.go](internal/reship/service.go)
