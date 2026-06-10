# ADR-0100: Use trait interfaces (CSVIngestor, IngestHook, APIProvider, …)

## Status

Accepted

## Context

Fat Plugin interface would force empty method implementations on every domain.

## Decision

Fine-grained trait interfaces; plugins implement only what they need.

## Consequences

Clean separation. Type-safe dispatch. Registry inspects implemented traits at registration.

## References

- [internal/plugin/plugin.go](internal/plugin/plugin.go)
