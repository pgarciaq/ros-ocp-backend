# ADR-0017: Use EMA-smoothed imbalance for stranded resource detection (α=0.3, threshold 0.6)

## Status

Accepted

## Context

Raw daily imbalance spikes from single-day anomalies.

## Decision

Exponential moving average with α=0.3 (~3-day effective smoothing window at daily samples); flag stranded only when EMA exceeds 0.6 sustained imbalance threshold.

## Alternatives Considered

### Raw instantaneous imbalance ratio
Single-day CPU/memory spikes flip stranded flags daily, generating alert fatigue. Rejected after observing >40% day-over-day flag churn on bursty namespaces.

### High α (e.g. 0.7)
Reacts to one-day anomalies almost immediately, recreating the noise problem. Rejected because α=0.3 balances ~3-day smoothing against timely detection of genuine sustained imbalance.

## Consequences

Noise-resistant detection. Delayed alerting on genuine imbalance (by design).

## References

- [internal/engine/recommend_nodes.go](internal/engine/recommend_nodes.go)
