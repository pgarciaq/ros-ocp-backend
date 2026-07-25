# ADR-0326: Calendar-accurate monthly hours for savings extrapolation

## Status

Accepted — supersedes [ADR-0182](0182-monthly-savings-730-hours.md)

## Phase

Accuracy

## Context

[ADR-0182](0182-monthly-savings-730-hours.md) established a fixed `HoursPerMonthInt = 730` constant
(≈30.4 days × 24 h) for extrapolating hourly savings deltas to monthly dollar estimates.
While this simplified comparison across months, it introduced a ±3% error relative to
actual calendar months (February has 672 hours; January/July have 744 hours).

This inaccuracy compounds in fleet savings summaries: a 100-cluster org with $50K/month in
savings sees up to $1,500 variance from reality. For customers using ROS savings as a
budgeting input, consistent over/under-estimation erodes trust.

All savings that use hourly extrapolation are affected:

| Resource type | Uses hourly extrapolation | Affected |
|---------------|--------------------------|----------|
| Container     | CPU/memory delta × rate × hours | Yes |
| Node          | CPU/memory delta × rate × hours | Yes |
| VM            | vCPU/memory delta × rate × hours | Yes |
| Namespace     | Aggregate delta × rate × hours | Yes |
| Quota / CRQ   | CPU/memory delta × rate × hours | Yes |
| PVC           | storage delta × rate/month | No (monthly rate, no hours multiplier) |
| Snapshot      | restoreSize × rate/month | No (monthly rate) |

## Decision

Replace the fixed `730` constant with `HoursInMonth(year, month)` — a deterministic
function that returns `daysInMonth(year, month) × 24`.

```go
func HoursInMonth(year int, month time.Month) int64 {
    days := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
    return int64(days) * 24
}
```

### Month context

The year/month is derived from `time.Now().UTC()` at the top-level pipeline entry points:

- **Ingestion pipeline** (`report_processor.go`): uses current UTC time when processing each manifest
- **Savings recalculation** (`savings_recalculate.go`): uses current UTC time when triggered
- **Threshold recalculation** (`threshold_recalculate.go`): uses current UTC time
- **Quota/CRQ runs** (`quota_run.go`, `cluster_quota_run.go`): uses current UTC time
- **API reprojection** (`quota_projection.go`): uses current UTC time

This means savings represent a **full calendar month projection**: "if you implement this
recommendation, you would save $X per month in the current month's calendar context."

### What is NOT stored

`hours_in_month` is not persisted in the database or returned in API responses. The value
is trivially derivable by any consumer that knows the month context (`daysInMonth × 24`),
and storing it would add a column to five recommendation tables for no analytical benefit.

### Backward compatibility

The legacy constant `HoursPerMonthInt = 730` remains in `core/savings_int.go` marked as
`// Deprecated`. Existing unit tests that assert exact numerical values continue to pass
`730` as the `hoursPerMonth` argument, preserving their original assertions without
modification.

## Alternatives Considered

### Keep 730 (status quo)

Simple but inaccurate. ±3% error is acceptable for individual recommendations but
compounds to meaningful dollar amounts at fleet scale.

### Store hours_in_month in the database

Adds a column to `container_recommendations`, `node_recommendations`,
`vm_recommendations`, `quota_recommendation_sets`, and `cluster_quota_recommendation_sets`.
The value is trivially derivable from the month context and provides no analytical benefit.
Rejected for unnecessary schema complexity.

### Use data observation period hours instead of calendar hours

Would produce "lookback" savings (what you actually saved during the observed period)
rather than "projection" savings (what you would save per month). The Fleet Summary UI
presents savings as "potential monthly savings" in an "X/month" format, which implies
projection. Using observation hours would produce confusing partial-month values early
in the month.

## Consequences

- Savings estimates are calendar-accurate (±0% vs the calendar month they represent)
- February savings are ~8% lower than January savings for the same workload, reflecting reality
- Year-over-year comparison of the same month is exact; cross-month comparison shows natural variance
- No database schema changes required
- No API response format changes required
- `HoursPerMonthInt` constant preserved as deprecated for backward compatibility

## Related Decisions

- [ADR-0182](0182-monthly-savings-730-hours.md): Superseded — original 730-hour constant
- [ADR-0291](0291-integer-micro-cents-savings-computation.md): Integer micro-cents pipeline (this change threads `hoursPerMonth` through the same functions)
- [ADR-0117](0117-savings-include-all-cost-types.md): Cost types included in savings
- [ADR-0040](0040-allow-negative-savings.md): Negative savings remain correctly signed

## References

- [`internal/engine/core/savings_int.go`](../../internal/engine/core/savings_int.go) — `HoursInMonth()` function
- [`internal/engine/core/savings_int_test.go`](../../internal/engine/core/savings_int_test.go) — Calendar-accuracy tests
- [GitHub issue #316](https://github.com/pgarciaq/ros-ocp-backend/issues/316) — Tracking issue
