# ADR-0264: Kruize-era legacy table background deletion strategy

## Status

Accepted

## Phase

7–8

## Context

After native engine replaced Kruize, legacy tables (`workload_metrics`, Kruize-specific columns) needed cleanup. Sources destroy events trigger org/cluster data deletion ([ADR-0156](0156-sources-destroy-event-cleanup.md)).

CASCADE on large legacy tables blocks the Kafka consumer transaction for minutes, causing consumer lag and potential rebalance storms.

## Decision

Background goroutine handles DROP/TRUNCATE of legacy tables asynchronously. Sources destroy event marks data for cleanup; background worker performs the actual deletion. At-most-once semantics — if worker crashes mid-delete, orphaned data may persist until next retention sweep.

## Alternatives Considered

### Synchronous CASCADE

Blocks Kafka consumer for minutes on large orgs.

### Immediate DROP in migration

Unsafe for running workloads mid-rollout.

### Scheduled cron job

Unnecessary complexity; event-driven cleanup sufficient.

## Consequences

- Non-blocking destroy event handling.
- Orphaned legacy data possible (acceptable — retention sweep catches it).
- Operational runbook documents manual cleanup procedure ([ADR-0136](0136-operational-runbooks-adversarial-review.md)).

## Related Decisions

- [ADR-0156](0156-sources-destroy-event-cleanup.md): Sources destroy cleanup.
- [ADR-0234](0234-no-soft-delete-cluster-state.md): No soft-delete cluster state.
- [ADR-0132](0132-retention-policies-per-table.md): Retention policies.
- [ADR-0263](0263-stop-writing-workload-metrics-in-native-mode.md): Stop workload_metrics writes.

## References

- [internal/housekeeper/cleanup.go](../../internal/housekeeper/cleanup.go)
- [docs/operations/runbooks/source-destroy-cleanup.md](../../docs/operations/runbooks/source-destroy-cleanup.md)
