# ADR-0113: Use NilCostDataProvider when Masu unavailable

## Status

Accepted

## Context

Cost data unavailability shouldn't fail ingest entirely.

## Decision

Nil provider returns zero rates; recommendations still generated without savings.

## Alternatives Considered

### Fail ingest entirely when Masu unavailable
Data loss for all recommendations during Masu outages; operators lose right-sizing guidance when they need it most.

### Block API responses until cost data returns
Availability loss on read path; list/detail endpoints 503 while ingest may have succeeded with usage data.

### Return stale cached rates silently
Wrong savings numbers without notification; operators act on outdated dollar figures.

## Related Decisions

- [ADR-0114](0114-notif-no-cost-data-container-node-pvc.md): emits notification when cost data is missing so UI shows $0 with explicit code.
- [ADR-0160](0160-savings-estimates-kill-switch.md): kill switch can disable savings estimates entirely when cost integration is broken.

## Consequences

Graceful degradation. Recommendations work without Masu. Savings show as $0 (notification code emitted).

## References

- [internal/costdata/provider.go](internal/costdata/provider.go)
