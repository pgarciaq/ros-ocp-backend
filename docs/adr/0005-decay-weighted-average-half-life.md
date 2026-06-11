# ADR-0005: Use decay-weighted average with configurable half-life per term

## Status

Accepted

## Context

Needed to bias recommendations toward recent behavior without dropping history entirely.

## Decision

Hour-based exponential decay with configurable half-life per term. Avoids DST calendar-day issues. Default half-lives: short term 7 days, medium term 15 days, long term 30 days—recent behavior dominates within one window while older samples still contribute at reduced weight.

## Alternatives Considered

### Simple moving average (equal weight)
Every sample in the lookback window contributes equally, so stale spikes from weeks ago skew recommendations as much as yesterday's telemetry. Rejected because FinOps users expect right-sizing to reflect current workload patterns.

### Exponential smoothing without configurable decay
A fixed smoothing factor hides the tuning knob operators need when short-term volatility differs from long-term baselines. Opaque α values make support and regression analysis harder than explicit half-life per term.

## Consequences

Recent data weighted more heavily. Half-life tunable per deployment. Requires decay math in hot path.

## References

- [internal/engine/decay.go](internal/engine/decay.go)
- [internal/engine/term_config.go](internal/engine/term_config.go)
