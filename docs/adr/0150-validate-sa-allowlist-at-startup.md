# ADR-0150: Validate empty SA allowlist blocks api-mode tag auth in prod

## Status

Accepted

## Context

Empty allowlist would accept any service account token.

## Decision

Startup validation fails if SA allowlist is empty in api mode with tag sync enabled.

## Consequences

Forces explicit allowlist configuration. Prevents open internal endpoints.

## References

- [internal/tags/auth_config.go](internal/tags/auth_config.go)
