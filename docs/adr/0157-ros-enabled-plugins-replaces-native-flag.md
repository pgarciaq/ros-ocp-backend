# ADR-0157: Replace ROS_USE_NATIVE_ENGINE with ROS_ENABLED_PLUGINS + Kruize exclusivity

## Status

Accepted

## Context

Parallel boolean + allowlist semantics were confusing and error-prone.

## Decision

Single `ROS_ENABLED_PLUGINS` mechanism with Kruize mutual exclusivity. Deprecated `ROS_USE_NATIVE_ENGINE=true` maps to `ROS_ENABLED_PLUGINS=containers,pvc,nodes,...` at startup.

## Consequences

One config point. Clear semantics. Legacy env var mapped at startup with deprecation warning.

## Migration Notes

Replace `ROS_USE_NATIVE_ENGINE=true` with explicit `ROS_ENABLED_PLUGINS=containers,pvc,nodes,gpu,...` (full plugin list for your deployment). Both variables are supported during transition; startup logs a deprecation warning when the legacy flag is set. Remove `ROS_USE_NATIVE_ENGINE` after all environments (SaaS, stage, on-prem) have updated Helm values and ConfigMaps. Kruize remains mutually exclusive—do not enable `kruize` alongside native plugins.

## References

- [internal/plugin/registry.go](internal/plugin/registry.go)
