# ADR-0162: Use housekeeper graceful shutdown with configurable grace period

## Status

Accepted

## Context

SIGKILL mid-batch leaves partial cleanup and orphaned data.

## Decision

Housekeeper respects context cancellation with configurable grace period before forced exit.

## Consequences

Clean shutdown. In-progress batches complete within grace. Configurable per deployment.

## References

- [internal/services/housekeeper/](internal/services/housekeeper/)
