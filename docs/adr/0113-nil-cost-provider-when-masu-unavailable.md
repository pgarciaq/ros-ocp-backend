# ADR-0113: Use NilCostDataProvider when Masu unavailable

## Status

Accepted

## Context

Cost data unavailability shouldn't fail ingest entirely.

## Decision

Nil provider returns zero rates; recommendations still generated without savings.

## Consequences

Graceful degradation. Recommendations work without Masu. Savings show as $0 (notification code emitted).

## References

- [internal/costdata/provider.go](internal/costdata/provider.go)
