# ADR-0074: Use manual OpenAPI + contract tests, not code-first codegen

## Status

Accepted

## Context

Generated specs from Echo annotations drift and produce less readable schemas.

## Decision

Hand-maintained `openapi.json` validated by contract tests against live handlers. Contract tests in `internal/api/openapi_contract_test.go` validate response shapes against `openapi.json` for **all registered plugin routes** — not just core endpoints (incorporates former ADR-0141). Bruno-only manual verification is insufficient for CI; spec drift must fail builds.

## Alternatives Considered

### oapi-codegen from OpenAPI spec
Generating server stubs and types from `openapi.json` enforces spec compliance, but the ROS API has plugin-specific response variants and manual enrichment paths that fight generated interfaces—handlers in `internal/api/` would become thin wrappers around inflexible generated code.

### swag or Echo annotation-driven spec (code-first)
Comment-based spec generation from Go handler annotations drifts within weeks as developers add query params without updating comments; the generated schemas also produce unreadable `$ref` chains unsuitable for koku-ui client generation.

### Schema-first codegen with runtime validation middleware
Generating both Go types and JSON Schema validators from one source guarantees compile-time safety, but runtime validation middleware adds latency on hot list paths and still diverges when handlers return plugin-specific fields not expressible in a single generated struct.

## Consequences

Full control over spec quality. Requires discipline to keep in sync. Contract tests catch drift on every plugin endpoint when the spec changes intentionally or accidentally.

## Related Decisions

- [ADR-0073](0073-dynamic-openapi-x-plugin-required.md): Dynamic OpenAPI filtered by x-plugin-required.
- [ADR-0249](0249-advisory-openapi-changelog-ci-non-blocking.md): Advisory OpenAPI changelog CI.

## References

- [openapi.json](../../openapi.json)
- [internal/api/openapi_contract_test.go](../../internal/api/openapi_contract_test.go)
