# ADR-0065: Preserve Kruize-compatible list/detail JSON shape for UI

## Status

Accepted

## Context

Greenfield API would break koku-ui without adapter layer during migration.

## Decision

Native engine produces response shapes matching Kruize API contract where possible.

## Alternatives Considered

### Greenfield REST API with BFF adapter in koku-ui
Designing a cleaner API and adapting in the frontend would improve field naming, but requires maintaining parallel response serializers in ROS and a translation layer in koku-ui—double maintenance during the multi-release migration window.

### Breaking API change with coordinated UI rewrite
A clean break avoids Kruize naming debt (`current_value`, nested `recommendation` blocks), but koku-ui HCCM and on-prem shell share components across SaaS and on-prem; a hard cutover exceeded the agreed migration timeline.

### GraphQL or gRPC replacement for REST list/detail
Modern RPC would reduce over-fetching, but koku-ui Axios clients, IQE contract tests, and Bruno collections all target the existing Kruize-shaped REST paths documented in `openapi.json`.

## Consequences

Seamless UI migration. Some awkward field naming inherited. Documented differences.

## References

- [internal/model/recommendation_set_native.go](internal/model/recommendation_set_native.go)
