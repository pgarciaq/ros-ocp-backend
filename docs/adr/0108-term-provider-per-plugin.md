# ADR-0108: Use TermProvider per plugin with different default windows

## Status

Accepted

## Context

PVC/storage grows slowly; container-like 1/7/15d windows are too short for storage.

## Decision

Each plugin declares its own term windows via TermProvider trait.

## Consequences

Resource-appropriate windows. PVC gets 7/30/90d. Container gets 1/7/15d. Configurable per-plugin.

## References

- [internal/plugins/pvc/plugin.go](internal/plugins/pvc/plugin.go)
