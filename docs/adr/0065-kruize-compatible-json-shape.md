# ADR-0065: Preserve Kruize-compatible list/detail JSON shape for UI

## Status

Accepted

## Context

Greenfield API would break koku-ui without adapter layer during migration.

## Decision

Native engine produces response shapes matching Kruize API contract where possible.

## Consequences

Seamless UI migration. Some awkward field naming inherited. Documented differences.

## References

- [internal/model/recommendation_set_native.go](internal/model/recommendation_set_native.go)
