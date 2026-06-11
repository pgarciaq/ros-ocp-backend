# ADR-0085: Use per-org threshold cache TTL 60s with async recalc on PUT

## Status

Accepted

## Context

Synchronous full-cluster recompute would block Settings API response.

## Decision

Cache thresholds 60s; PUT invalidates cache and triggers async recalculation. Sixty seconds balances freshness (operators see config changes within one minute) against compute cost (avoids P99 latency spikes from synchronous full-cluster recompute on every GET).

## Alternatives Considered

### Synchronous recompute on every settings read
P99 Settings API latency spikes to seconds on large orgs when threshold derivation scans all namespaces.

### Longer TTL (e.g. 5–15 minutes)
Stale thresholds miss rapid config changes during incident response; operators assume PUT took effect immediately.

## Consequences

Fast settings response. Eventually consistent recommendations (within 60s).

## References

- [internal/engine/threshold_settings.go](internal/engine/threshold_settings.go)
