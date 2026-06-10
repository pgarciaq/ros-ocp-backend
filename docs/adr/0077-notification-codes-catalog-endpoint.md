# ADR-0077: Use GET /notification-codes public catalog

## Status

Accepted

## Context

Embedding full code dictionary in every list response wastes bandwidth.

## Decision

Separate catalog endpoint; responses include code numbers only.

## Consequences

Smaller responses. Client fetches catalog once. Must version catalog on changes.

## References

- [internal/api/handlers_notification_codes.go](internal/api/handlers_notification_codes.go)
