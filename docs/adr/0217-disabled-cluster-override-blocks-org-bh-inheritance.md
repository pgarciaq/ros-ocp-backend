# ADR-0217: Disabled cluster override blocks org BH inheritance for digests

## Status

Accepted

## Context

When a cluster has an explicit "disabled" business hours override, it should NOT inherit the org-level schedule. This is distinct from "no cluster config" (which DOES inherit org defaults).

## Decision

`ProducesBusinessHoursDigests()` resolution:

1. Any enabled namespace → true (BH digests produced).
2. Else cluster row (if present and disabled) wins over org — explicit disabled suppresses org inheritance.
3. No cluster row → inherit from org.

## Alternatives Considered

### No disabled state

Cannot opt a cluster out of org-wide BH without per-namespace configuration.

### Per-namespace only

Too granular for operators managing cluster-level policy.

## Consequences

- Three states per cluster: explicit schedule, explicit disabled, no config (inherits org).
- Support must distinguish "disabled override" from "no config."
- Digest production logic diverges from schedule display inheritance in edge cases.

## Related Decisions

- [ADR-0216](0216-business-hours-pending-marker-stub-rows.md): Stub rows and schedule loading.
- [ADR-0036](0036-business-hours-container-namespace-only.md): BH scope for container/namespace.

## References

- [internal/bhschedule/digest_production.go](../../internal/bhschedule/digest_production.go)
