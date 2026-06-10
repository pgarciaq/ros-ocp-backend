# ADR-0074: Use manual OpenAPI + contract tests, not code-first codegen

## Status

Accepted

## Context

Generated specs from Echo annotations drift and produce less readable schemas.

## Decision

Hand-maintained `openapi.json` validated by contract tests against live handlers.

## Consequences

Full control over spec quality. Requires discipline to keep in sync. Contract tests catch drift.

## References

- [openapi.json](openapi.json)
- [internal/api/openapi_contract_test.go](internal/api/openapi_contract_test.go)
