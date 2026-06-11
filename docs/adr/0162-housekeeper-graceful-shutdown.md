# ADR-0162: Use housekeeper graceful shutdown with configurable grace period

## Status

Accepted

## Context

SIGKILL mid-batch leaves partial cleanup and orphaned data.

## Decision

Housekeeper respects context cancellation with configurable grace period before forced exit.

## Consequences

Clean shutdown. In-progress batches complete within grace. Configurable per deployment.

## Related Patterns

`internal/asyncjobs` provides the API/processor-mode equivalent: shared WaitGroup, `RegisterShutdownHook()` for component drain, configurable grace period, and `Context()` for background work that respects SIGTERM.

The synthesized manifest recommendation debouncer registers via `asyncjobs.RegisterShutdownHook()` ([ADR-0165](0165-defer-recommendations-for-synthesized-manifests.md)).

## References

- [internal/services/housekeeper/](internal/services/housekeeper/)
- [internal/asyncjobs/](internal/asyncjobs/)
