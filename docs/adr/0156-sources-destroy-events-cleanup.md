# ADR-0156: Use Sources destroy events for tenant cleanup

## Status

Accepted

## Context

Polling for deleted sources is unreliable and delayed.

## Decision

React to Platform Sources destroy events via Kafka for immediate cleanup.

## Alternatives Considered

### Polling Sources API
Overhead on every housekeeper cycle plus minutes-to-hours delay before deleted tenants are purged.

### TTL-based cleanup
Arbitrary expiry leaves orphaned data indefinitely for active-then-deleted sources, or deletes too aggressively if TTL is short.

### Manual operator cleanup
Error-prone; forgotten org schemas accumulate storage and skew fleet metrics.

## Consequences

Near-real-time cleanup. Depends on Sources event delivery. Housekeeper catches stragglers if destroy event is missed—monitor `rosocp_housekeeper_cleanup_*` metrics and orphaned-org counts.

## References

- [internal/services/housekeeper/sourcesCleaner.go](internal/services/housekeeper/sourcesCleaner.go)
