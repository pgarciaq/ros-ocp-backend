# ADR-0035: Use business-hours as nested block, not separate API rows

## Status

Accepted

## Context

Separate list resources for BH would double API surface and client complexity.

## Decision

BH recommendations nested as `business_hours` block inside container/namespace responses.

## Alternatives Considered

### Separate API resources for business-hours config
Extra round-trips to assemble container + BH perspective; koku-ui would need parallel pagination and merge logic.

### Cron expression in API
Powerful but too complex for settings UI; error-prone for operators who think in weekday/time blocks, not cron syntax.

## Related Decisions

- [ADR-0127](0127-dual-digest-schedule-type-column.md): dual digest streams (24×7 vs business-hours) feed the nested BH block.

## Consequences

Single API call returns both perspectives. Slightly more complex response schema. No separate pagination.

## References

- [docs/features-business-hours.md](docs/features-business-hours.md)
