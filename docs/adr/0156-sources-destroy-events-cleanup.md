# ADR-0156: Use Sources destroy events for tenant cleanup

## Status

Accepted

## Context

Polling for deleted sources is unreliable and delayed.

## Decision

React to Platform Sources destroy events via Kafka for immediate cleanup.

## Consequences

Near-real-time cleanup. Depends on Sources event delivery. Housekeeper catches stragglers.

## References

- [internal/services/housekeeper/sourcesCleaner.go](internal/services/housekeeper/sourcesCleaner.go)
