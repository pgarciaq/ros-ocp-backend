# ADR-0175: Idle API surfaces waste (not savings) with terminate guidance

## Status

Accepted

## Context

Idle and zombie containers represent full waste of their allocated resources, not partial over-provisioning. The savings concept—a downsize delta between current and recommended requests—does not apply. The correct optimization is termination.

Mixing idle waste into savings totals would confuse dashboard math ([ADR-0040](0040-allow-negative-savings.md) covers rightsizing deltas only).

## Decision

For non-active containers, the API:

- Clears `estimated_monthly_savings`
- Surfaces `estimated_monthly_waste` (100% of current allocation cost)
- Uses `BuildIdleRecommendation()` to always recommend `"terminate"` with confidence high (≥14 idle days) or medium (7–13)

Waste uses the same monthly extrapolation constant as savings ([ADR-0182](0182-monthly-savings-730-hours.md)).

## Alternatives Considered

### Treat waste as savings

Confuses total savings math and misleads users about recoverable rightsizing opportunity.

### Negative savings

Semantically wrong—idle is not a downsize recommendation.

## Consequences

- UI must handle two monetary fields: savings (rightsizing) and waste (idle).
- Fleet aggregation sums waste separately from savings to avoid double-counting ([ADR-0183](0183-separate-estimated-waste-cents.md)).
- Idle rows never show positive savings in list or detail responses.

## Related Decisions

- [ADR-0040](0040-allow-negative-savings.md): Negative savings allowed for rightsizing only.
- [ADR-0182](0182-monthly-savings-730-hours.md): Monthly extrapolation methodology.

## References

- [internal/model/idle_api.go](../../internal/model/idle_api.go)
