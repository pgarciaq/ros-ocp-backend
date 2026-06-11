# ADR-0145: Deny private networks on CSV URL fetch unless development

## Status

Accepted

## Context

Malicious presigned URLs in Kafka messages could SSRF internal services.

## Decision

Block RFC1918/loopback/link-local URLs in production (IPv4 and IPv6 via `net.IP.IsPrivate()`, `IsLoopback()`, and link-local checks); allow in development mode.

## Alternatives Considered

### Hostname allowlist only (no DNS re-resolution)
Checking the URL host string against `ROS_CSV_ALLOWED_HOSTS` catches obvious attacks but misses DNS rebinding where a public hostname resolves to `10.0.0.0/8` at fetch time; `denyRestrictedHost` in `internal/utils/csv_security.go` resolves the host and rejects restricted IPs before the HTTP client connects.

### Block all non-HTTPS schemes
Rejecting `http://` would eliminate some SSRF vectors, but on-prem MinIO/NooBaa deployments use plain HTTP inside the cluster mesh; scheme blocking would break legitimate presigned URLs while still allowing HTTPS URLs that resolve to private IPs.

### Proxy all CSV fetches through a dedicated egress service
A sidecar proxy with fixed egress rules would centralize SSRF defense, but adds another deployment component to cost-onprem and duplicates the allowlist logic already enforced inline before `http.Get` in the processor.

## Consequences

SSRF mitigated. Dev mode unrestricted for local testing. Allowlist for legitimate internal URLs.

## References

- [internal/utils/csv_security.go](internal/utils/csv_security.go)
