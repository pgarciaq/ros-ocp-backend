# ADR-0118: Invalidate cost cache on threshold settings change

## Status

Accepted

## Context

Threshold changes may affect which rates are relevant for display.

## Decision

Settings PUT invalidates cost cache for affected org.

## Consequences

Fresh rates after settings change. One extra Masu call on next request.

## References

- [internal/engine/threshold_settings.go](internal/engine/threshold_settings.go)
