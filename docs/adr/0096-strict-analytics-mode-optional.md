# ADR-0096: Use strict analytics mode optional (ROS_INGEST_STRICT_ANALYTICS)

## Status

Accepted

## Context

Need both all-or-nothing consistency and degraded-mode resilience options.

## Decision

Default degraded (recommendations still served, flag set); optional strict (blocks commit on failure).

## Consequences

Operator choice between availability and consistency. Degraded is safe default.

## References

- [internal/engine/analytics_pipeline.go](internal/engine/analytics_pipeline.go)
