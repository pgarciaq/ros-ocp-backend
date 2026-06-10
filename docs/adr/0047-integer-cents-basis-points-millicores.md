# ADR-0047: Use integer cents / basis points / millicores, not floats

## Status

Accepted

## Context

Float money causes rounding errors in aggregation.

## Decision

All monetary values as integer cents, ratios as basis points, CPU as millicores.

## Consequences

Deterministic aggregation. Requires conversion at API boundary. No float surprises.

## References

- [internal/money/format.go](internal/money/format.go)
