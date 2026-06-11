# ADR-0071: Exclude GPU savings from savings-summary fleet total

## Status

Accepted

## Context

GPU time-slicing savings are computed at read-time, not persisted; including them would be inconsistent.

## Decision

Fleet `savings-summary` excludes GPU dollar amounts (`by_plugin.gpu=0`).

## Alternatives Considered

### Include GPU in fleet total
GPU time-slicing savings are per-slice and not additive across the fleet; summing them overstates total savings and double-counts shared hardware.

### Separate GPU savings endpoint only
UI cannot compose a single fleet dashboard without an extra merge step; operators expect one savings-summary call for overview widgets.

## Related Decisions

- [ADR-0115](0115-gpu-mig-idle-persist-timeslicing-read-time.md): GPU savings computed at read time, which is why fleet summary excludes persisted GPU totals.

## Consequences

Consistent fleet totals from persisted data only. GPU savings visible per-recommendation only.

## References

- [internal/api/handlers_savings_summary.go](internal/api/handlers_savings_summary.go)
