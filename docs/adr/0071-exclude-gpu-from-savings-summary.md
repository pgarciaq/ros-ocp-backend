# ADR-0071: Exclude GPU savings from savings-summary fleet total

## Status

Accepted

## Context

GPU time-slicing savings are computed at read-time, not persisted; including them would be inconsistent.

## Decision

Fleet `savings-summary` excludes GPU dollar amounts (`by_plugin.gpu=0`).

## Consequences

Consistent fleet totals from persisted data only. GPU savings visible per-recommendation only.

## References

- [internal/api/handlers_savings_summary.go](internal/api/handlers_savings_summary.go)
