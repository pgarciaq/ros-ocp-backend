# ADR-0062: Mark clusters analytics_incomplete when history/quality fails

## Status

Accepted

## Context

Silent partial success hides data-quality gaps from API consumers.

## Decision

Set `clusters.analytics_incomplete` flag and timestamp on history/quality write failures.

## Consequences

API consumers see degraded state. Operational alerting possible. Recommendations still served.

## References

- [internal/engine/cluster_analytics.go](internal/engine/cluster_analytics.go)
