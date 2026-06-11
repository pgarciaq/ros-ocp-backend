# ADR-0213: InitThresholdDefaults copies env into process-wide defaults at startup

## Status

Accepted

## Context

Env lock values must override database values at resolution time. The resolution function needs baseline defaults that incorporate `ROS_*_DEFAULT_*` environment variables without re-reading them on every request.

## Decision

`InitThresholdDefaults()` runs once at startup, reading all `ROS_*_DEFAULT_*` env vars and mutating package-level default structs. These defaults form the base layer in resolve-time precedence: env lock → DB → defaults.

Defaults are NOT re-read after startup.

## Alternatives Considered

### Read env at every resolve call

Performance penalty on hot path.

### Config struct injection

Major refactor of settings resolution across handlers and engine.

## Consequences

- Hot-reloading env vars requires process restart.
- Tests must call `InitThresholdDefaults()` or set package-level vars explicitly.
- Package-level mutation is not goroutine-safe during init (acceptable — single goroutine at startup).

## Related Decisions

- [ADR-0135](0135-centralized-viper-config.md): Centralized Viper config.
- [ADR-0208](0208-settings-scope-org-wide-only-except-business-hours.md): Resolution precedence.

## References

- [internal/engine/threshold_defaults.go](../../internal/engine/threshold_defaults.go)
