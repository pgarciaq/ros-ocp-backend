# ADR-0145: Deny private networks on CSV URL fetch unless development

## Status

Accepted

## Context

Malicious presigned URLs in Kafka messages could SSRF internal services.

## Decision

Block RFC1918/loopback/link-local URLs in production; allow in development mode.

## Consequences

SSRF mitigated. Dev mode unrestricted for local testing. Allowlist for legitimate internal URLs.

## References

- [internal/utils/csv_security.go](internal/utils/csv_security.go)
