# ADR-0005: Use decay-weighted average with configurable half-life per term

## Status

Accepted

## Context

Needed to bias recommendations toward recent behavior without dropping history entirely.

## Decision

Hour-based exponential decay with configurable half-life per term. Avoids DST calendar-day issues.

## Consequences

Recent data weighted more heavily. Half-life tunable per deployment. Requires decay math in hot path.

## References

- [internal/engine/decay.go](internal/engine/decay.go)
- [internal/engine/term_config.go](internal/engine/term_config.go)
