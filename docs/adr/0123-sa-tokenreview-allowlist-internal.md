# ADR-0123: Use SA TokenReview allowlist for internal endpoints

## Status

Accepted

## Context

User JWTs on service-to-service internal routes is inappropriate.

## Decision

Internal tag/savings endpoints authenticated via Kubernetes SA TokenReview with allowlist.

## Consequences

Service-to-service auth. No user tokens on internal paths. Allowlist validates at startup.

## References

- [internal/tags/auth.go](internal/tags/auth.go)
