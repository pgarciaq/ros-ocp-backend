# ADR-0067: Encode cursors as base64url JSON

## Status

Accepted

## Context

Needed opaque but debuggable cursor format.

## Decision

base64url-encoded JSON of sort key values. Not encrypted (no secret data in sort keys).

## Consequences

Debuggable. Compact. Not tamper-proof (acceptable since server validates).

## References

- [internal/api/cursor.go](internal/api/cursor.go)
