# ADR-0244: Request correlation via Echo request_id (no OpenTelemetry)

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Context

API debugging requires correlating log lines across middleware, handlers, and downstream Masu calls. [ADR-0133](0133-structured-logging-zerolog.md) mandates structured fields including `request_id`.

Full distributed tracing (OpenTelemetry spans, trace context propagation) is not implemented in ROS. Platform may pass `x-rh-insights-request-id` from upstream gateways.

## Decision

Use Echo's built-in **`request_id` middleware** (UUID per HTTP request). `logging.ForRequest` injects `request_id` plus identity context (`org_id`, account) into zerolog loggers for the request scope.

**No OpenTelemetry** span creation or propagation is implemented in ROS. Cross-service correlation relies on:

- Passthrough of `x-rh-insights-request-id` on outbound HTTP where handlers attach it manually
- Shared `request_id` in API logs for single-service request tracing

Processor and Kafka paths use manifest/cluster correlation fields instead of HTTP request_id.

## Alternatives Considered

### OpenTelemetry SDK throughout

Operational overhead and collector deployment not justified for current scale.

### Custom header only, no Echo middleware

Duplicates Echo's stable UUID generation and ordering guarantees.

### Trace IDs in API response body

Leaks internal correlation to clients unnecessarily; logs sufficient.

## Consequences

- Support searches logs by `request_id` from API error responses when exposed.
- No automatic trace waterfall across Koku ↔ ROS; manual header correlation only.
- Future OTel adoption would require new ADR superseding this decision.

## Related Decisions

- [ADR-0133](0133-structured-logging-zerolog.md): Structured logging with zerolog.
- [ADR-0192](0192-echo-route-registration-order-middleware-layering.md): Middleware layering order.

## References

- [internal/logging/context.go](../../internal/logging/context.go)
- [internal/api/server.go](../../internal/api/server.go)
