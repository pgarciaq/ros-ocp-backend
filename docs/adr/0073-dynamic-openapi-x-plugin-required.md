# ADR-0073: Use dynamic OpenAPI filtered by x-plugin-required

## Status

Accepted

## Context

Disabled plugin routes shouldn't appear in API documentation.

## Decision

OpenAPI spec annotated with `x-plugin-required`; handler filters disabled routes at runtime.

## Consequences

Clean API docs per deployment. Spec varies by config. Must test with/without plugins.

## References

- [internal/api/openapi_handler.go](internal/api/openapi_handler.go)
