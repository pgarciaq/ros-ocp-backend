# ADR-0280: Fixed-point savings migration (float → integer cents)

## Status

Accepted

## Phase

9–12

## Context

Early savings used `float64` for dollar amounts. Floating-point arithmetic causes rounding errors in fleet aggregation across thousands of containers. `MoneyAmount` API type ([ADR-0064](0064-money-amount-api-cents-internal.md)) needs precise cents representation.

## Decision

Migrate savings storage from `float64` columns to integer cents (`estimated_savings_cents`, `estimated_waste_cents`). Migration path: add integer columns → dual-write during transition → drop float columns. API always returns structured `MoneyAmount` with value + units.

## Alternatives Considered

### Keep float64

Accumulating precision errors in fleet totals.

### NUMERIC/DECIMAL PostgreSQL type

Slower arithmetic, larger storage.

### Big-bang migration

Risky for large partitioned tables.

## Consequences

- Precise aggregation (no floating-point drift).
- Migration required dual-write phase.
- Historical float data converted with rounding.
- All new savings code uses cents exclusively ([ADR-0047](0047-integer-cents-basis-points-millicores.md)).

## Related Decisions

- [ADR-0047](0047-integer-cents-basis-points-millicores.md): Integer cents representation.
- [ADR-0064](0064-money-amount-api-cents-internal.md): MoneyAmount API type.
- [ADR-0040](0040-allow-negative-savings.md): Allow negative savings.

## References

- [migrations/000062_savings_cents.up.sql](../../migrations/000062_savings_cents.up.sql)
- [internal/cost/savings.go](../../internal/cost/savings.go)
