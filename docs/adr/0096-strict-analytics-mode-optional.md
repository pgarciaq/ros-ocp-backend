# ADR-0096: Use strict analytics mode optional (ROS_INGEST_STRICT_ANALYTICS)

## Status

Accepted

## Context

Need both all-or-nothing consistency and degraded-mode resilience options.

## Decision

Default degraded (recommendations still served, flag set); optional strict (blocks commit on failure).

## Alternatives Considered

### Always-strict analytics (fail ingest on any history/quality error)
All-or-nothing consistency prevents serving recommendations built on incomplete analytics, but broke existing on-prem deployments where transient PG timeouts during history writes aborted entire manifests; finding #45 later changed the default to strict for new installs while preserving the env toggle.

### Always-degraded (never block on analytics failure)
Maximizing availability avoids ingest aborts, but SaaS operators reported silent data-quality gaps—quality scores and adoption metrics displayed as authoritative when underlying history rows were missing, with no `analytics_incomplete` signal for monitoring.

### Strict per-cluster with partial manifest commit
Blocking only failed clusters while committing others sounds balanced, but Kafka messages are cluster-scoped manifests—partial commit within one message complicates offset semantics and idempotent replay in `analytics_pipeline.go`.

## Consequences

Operator choice between availability and consistency. Degraded is safe default.

## References

- [internal/engine/analytics_pipeline.go](internal/engine/analytics_pipeline.go)
