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

## IPv6 Coverage (commit `18beb896`)

Initial implementation blocked IPv4 RFC1918, loopback, and link-local ranges. Commit `18beb896` extended checks to IPv6 using standard library helpers in `denyRestrictedHost`:

- `net.IP.IsPrivate()` — blocks IPv6 ULA (`fc00::/7`) in addition to IPv4 private ranges
- `net.IP.IsLoopback()` — blocks `::1` and IPv4 loopback
- `net.IP.IsLinkLocalUnicast()` — blocks IPv6 link-local (`fe80::/10`)
- `net.IP.IsLinkLocalMulticast()` — blocks link-local multicast

DNS resolution is performed before fetch; restricted IPs block the request even when the URL hostname appears public (rebinding defense).

## Allowlist precedence

Hosts matching the explicit allowlist (`ROS_CSV_ALLOWED_HOSTS`) bypass the private-network deny check. This allows on-prem deployments to use in-cluster storage services (e.g., MinIO) that resolve to private ClusterIPs. The allowlist is an intentional trust decision; untrusted hosts still receive full SSRF protection including DNS re-resolution.

## References

- [internal/utils/csv_security.go](../../internal/utils/csv_security.go)
