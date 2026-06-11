# ADR-0207: Stdlib JSON encoding for API responses (not jsoniter/sonic)

## Status

Accepted

## Context

High-performance JSON libraries (jsoniter, sonic) offer 2–5× encoding speed. The API serves JSON responses for all endpoints. Response bodies are typically small; network latency dominates end-to-end latency.

## Decision

Use stdlib `encoding/json` for all API responses.

Benchmarked <10% real-world gain (response bodies are small, network latency dominates). Compatibility risk with Kruize-compatible response shapes ([ADR-0065](0065-kruize-compatible-json-shape.md)) and custom `MarshalJSON` implementations outweighs marginal performance.

## Consequences

- No special build tags or unsafe code.
- Custom `MarshalJSON` methods work reliably.
- <10% slower than possible — acceptable for current scale.
- Revisit if response body sizes grow significantly (e.g., bulk export).

## Alternatives Considered

### jsoniter

2–5× faster but stdlib-incompatible edge cases with custom marshalers. Rejected.

### sonic

Requires amd64, uses unsafe, breaks on ARM (aarch64 SNO deployments). Rejected.

### Per-endpoint library choice

Maintenance burden and inconsistent behavior. Rejected.

## Related Decisions

- [ADR-0065](0065-kruize-compatible-json-shape.md): Kruize-compatible JSON response format.

## References

- [internal/api/server.go](../../internal/api/server.go)
