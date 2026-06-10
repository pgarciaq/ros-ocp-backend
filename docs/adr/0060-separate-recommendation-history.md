# ADR-0060: Separate recommendation_history from live recommendation_sets

## Status

Accepted

## Context

Overwriting history on each ingest prevents quality/stability metrics over time.

## Decision

Maintain separate `recommendation_history` table preserving all past states.

## Consequences

Enables quality metrics, adoption tracking, stability scoring. More storage.

## References

- [internal/engine/history.go](internal/engine/history.go)
