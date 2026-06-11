# ADR-0193: No public API rate limiting; body limits on internal sync only

## Status

Accepted

## Context

Rate limiting at the application level adds complexity and may conflict with infrastructure-level rate limiting (3scale, HAProxy). The threat model assumes ingress handles abuse. Internal tag sync, however, accepts bulk uploads that need size bounds.

## Decision

- **No global rate limiter** in the application — DDoS and abuse protection is an infrastructure responsibility.
- **Body size limit** (`ROS_CSV_MAX_BODY_BYTES`, default 100 MiB) applies only to the internal tag sync endpoint, which receives bulk data.
- **CORS** denies all origins in non-development when `ROS_CORS_ALLOWED_ORIGINS` is unset ([ADR-0135](0135-centralized-viper-config.md)).

This is an explicit non-decision documented for future reference.

## Consequences

- Application trusts ingress for rate limiting.
- DDoS protection is infrastructure responsibility.
- Internal endpoints have body limits because they accept uploads.
- Explicit non-decision documented so future contributors do not reintroduce duplicate limiters.

## Alternatives Considered

### Application-level rate limiting

Duplicates infrastructure controls and adds state (token buckets, Redis). Rejected.

### Per-endpoint limits

Maintenance burden and hard to tune per deployment. Rejected.

## Related Decisions

- [ADR-0135](0135-centralized-viper-config.md): CORS configuration via centralized Viper config.

## References

- [internal/api/server.go](../../internal/api/server.go)
