# ADR-0147: Escape ILIKE wildcards in filter values

## Status

Accepted

## Context

Raw user input in SQL ILIKE patterns enables regex-like injection.

## Decision

Escape `%`, `_`, `\` in all user-provided filter values before ILIKE queries.

## Consequences

No wildcard injection. Users can't use ILIKE patterns (acceptable for this API).

## References

- [internal/api/utils.go](internal/api/utils.go)
