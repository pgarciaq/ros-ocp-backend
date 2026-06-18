# ADR-0157: Replace ROS_USE_NATIVE_ENGINE with ROS_ENABLED_PLUGINS + Kruize exclusivity

## Status

Completed — `ROS_USE_NATIVE_ENGINE` has been fully removed. The native engine
is unconditionally active unless `ROS_ENABLED_PLUGINS=kruize` is set explicitly.

## Context

Parallel boolean + allowlist semantics were confusing and error-prone.

## Decision

Single `ROS_ENABLED_PLUGINS` mechanism with Kruize mutual exclusivity. Deprecated `ROS_USE_NATIVE_ENGINE=true` maps to `ROS_ENABLED_PLUGINS=containers,pvc,nodes,...` at startup.

## Resolution

The transition period is complete. All downstream charts and environments have
migrated to `ROS_ENABLED_PLUGINS` / `ROS_DISABLED_PLUGINS`. The legacy
`ROS_USE_NATIVE_ENGINE` environment variable, the `UseNativeEngine` config field,
and the `ApplyLegacyUseNativeEngineEnv()` translation function have been removed.

If an environment still sets `ROS_USE_NATIVE_ENGINE`, it will be silently ignored
(Viper unmarshal skips unknown env vars when not bound to a field).

## Consequences

One config point. Clear semantics. No legacy translation code.

## References

- [internal/plugin/registry.go](internal/plugin/registry.go)
