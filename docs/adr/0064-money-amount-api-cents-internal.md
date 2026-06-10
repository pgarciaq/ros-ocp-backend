# ADR-0064: Use MoneyAmount (value+units) in API while storing cents internally

## Status

Accepted

## Context

Float JSON dollars caused rounding bugs in display after arithmetic.

## Decision

Internal: integer cents. External API: structured `{value: "1.23", units: "USD"}`.

## Consequences

Deterministic storage. Clean API contract. Conversion at boundary only.

## References

- [internal/money/format.go](internal/money/format.go)
