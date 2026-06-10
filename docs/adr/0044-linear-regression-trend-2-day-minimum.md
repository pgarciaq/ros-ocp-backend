# ADR-0044: Use linear regression trend with ≥2-day minimum

## Status

Accepted

## Context

Trend detection needs minimum data; zero-slope on short windows misleads.

## Decision

Require at least 2 days of data for linear regression trend. Emit no trend otherwise.

## Consequences

Avoids false trends. Short-window recommendations lack trend signals.

## References

- [internal/engine/trend.go](internal/engine/trend.go)
