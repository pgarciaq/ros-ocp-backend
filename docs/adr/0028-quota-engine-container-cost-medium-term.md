# ADR-0028: Fix quota engine to container cost/medium_term aggregates

## Status

Accepted

## Context

Quota recommendations need to aggregate right-sized workload demand, not raw namespace usage.

## Decision

Quota engine sums container cost-engine medium-term recommendations per namespace.

## Consequences

Quota reflects optimized demand. Depends on container recs existing first (ordering constraint).

## References

- [internal/engine/recommend_quota.go](internal/engine/recommend_quota.go)
