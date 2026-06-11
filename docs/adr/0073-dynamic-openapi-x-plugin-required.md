# ADR-0073: Use dynamic OpenAPI filtered by x-plugin-required

## Status

Accepted

## Context

Disabled plugin routes shouldn't appear in API documentation.

## Decision

OpenAPI spec annotated with `x-plugin-required`; handler filters disabled routes at runtime.

## Alternatives Considered

### Separate spec files per deployment
Maintenance burden—every plugin toggle requires regenerating and shipping multiple OpenAPI artifacts; drift between SaaS and on-prem docs.

### Static spec with "may return 404" notes
Confuses API consumers and codegen tools that generate clients for routes that don't exist in their deployment.

### Runtime spec generation from code reflection
Fragile—handler registration order and middleware wrappers produce incomplete or unstable schemas across refactors.

## Consequences

Clean API docs per deployment. Spec varies by config. Must test with/without plugins.

## References

- [internal/api/openapi_handler.go](internal/api/openapi_handler.go)
