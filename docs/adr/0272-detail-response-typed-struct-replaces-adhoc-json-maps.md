# ADR-0272: DetailResponse typed struct replaces ad-hoc JSON maps

## Status

Accepted

## Phase

5

## Context

Early native detail API returned hand-constructed `map[string]interface{}` for Kruize JSON shape compatibility ([ADR-0065](0065-kruize-compatible-json-shape.md)). This was error-prone, untestable against OpenAPI, and hard to maintain as fields grew.

Missing keys and type mismatches surfaced only at runtime in integration tests.

## Decision

Phase 5 introduced `DetailResponse` struct with typed fields matching OpenAPI schema. `BuildDetailResponse()` assembles from model data. Enables compile-time correctness and contract test validation.

## Alternatives Considered

### Keep map construction

Ongoing bugs; no compile-time safety.

### Code generation from OpenAPI

Build complexity; generated code hard to debug and customize.

## Consequences

- Breaking change in internal response assembly (not external API).
- All handlers updated to use struct builder.
- OpenAPI contract tests validate struct output.
- Future field additions are type-safe.

## Related Decisions

- [ADR-0065](0065-kruize-compatible-json-shape.md): Kruize-compatible JSON shape.
- [ADR-0074](0074-manual-openapi-contract-tests.md): Manual OpenAPI contract tests.
- [ADR-0273](0273-subquery-pagination-replacing-row-multiplier.md): Subquery pagination.

## References

- [internal/api/detail_response.go](../../internal/api/detail_response.go)
- [internal/api/handlers_detail.go](../../internal/api/handlers_detail.go)
