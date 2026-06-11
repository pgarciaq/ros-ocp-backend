# ADR-0123: Use SA TokenReview allowlist for internal endpoints

## Status

Accepted

## Context

User JWTs on service-to-service internal routes is inappropriate.

## Decision

Internal tag/savings endpoints authenticated via Kubernetes SA TokenReview with allowlist.

## Alternatives Considered

### Shared secret header
Rotation pain across Koku ↔ ROS deployments; secrets in ConfigMaps violate least-privilege and audit requirements.

### mTLS between services
Certificate management overhead on-prem (custom CA, renewal, pod volume mounts) exceeds team capacity for internal-only routes.

### No auth on internal routes
Finding #37 risk—any pod in the cluster could trigger tag sync or savings recalc, causing data corruption or DoS.

## Consequences

Service-to-service auth via K8s SA identity without shared secrets. Allowlist validated at startup. Threat model: without TokenReview, compromised workloads in unrelated namespaces could call internal endpoints; TokenReview binds caller identity to an allowlisted ServiceAccount name.

## References

- [internal/tags/auth.go](internal/tags/auth.go)
