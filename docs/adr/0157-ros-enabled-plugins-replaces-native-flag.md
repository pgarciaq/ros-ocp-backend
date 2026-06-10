# ADR-0157: Replace ROS_USE_NATIVE_ENGINE with ROS_ENABLED_PLUGINS + Kruize exclusivity

## Status

Accepted

## Context

Parallel boolean + allowlist semantics were confusing and error-prone.

## Decision

Single `ROS_ENABLED_PLUGINS` mechanism with Kruize mutual exclusivity.

## Consequences

One config point. Clear semantics. Legacy env var mapped at startup with deprecation warning.

## References

- [internal/plugin/registry.go](internal/plugin/registry.go)
