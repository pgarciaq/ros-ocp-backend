# ADR-0129: Use separate processes (api, processor, housekeeper, poller) from one binary

## Status

Accepted

## Context

Monolith process can't scale API and processing independently.

## Decision

Multi-mode Cobra binary; Kubernetes deploys separate Deployments per mode.

## Consequences

Independent scaling. Shared codebase. Single container image. Mode selected by command.

## References

- [cmd/start.go](cmd/start.go)
