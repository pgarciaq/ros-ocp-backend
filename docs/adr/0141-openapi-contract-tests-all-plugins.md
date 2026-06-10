# ADR-0141: Use OpenAPI contract tests on every plugin endpoint

## Status

Accepted

## Context

Manual Bruno-only verification insufficient for CI.

## Decision

Contract tests validate response shapes against openapi.json for all registered routes.

## Consequences

Spec drift caught in CI. Must update tests when spec changes intentionally.

## References

- [internal/api/openapi_contract_test.go](internal/api/openapi_contract_test.go)
