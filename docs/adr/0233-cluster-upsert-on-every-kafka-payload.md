# ADR-0233: Cluster upsert on every Kafka payload before file processing

## Status

Accepted

## Context

Kafka messages from the HCCM pipeline ([ADR-0089](0089-manual-kafka-commit-after-success.md)) carry report manifests with one or more CSV files. Downstream ingest hooks, file status tracking ([ADR-0166](0166-report-file-status-manifest-gating.md)), and recommendation produce assume a `Cluster` row exists for the payload's cluster UUID.

Historical deployments used separate `clusters` and `workloads` tables alongside the native schema. New ingest paths must not fail on missing cluster metadata when the first file in a batch references a cluster that has not yet been seen in the API layer.

## Decision

`report_processor.go` upserts `RHAccount` and `Cluster` with `last_reported_at = now()` **before** iterating manifest files. Every Kafka payload refreshes cluster liveness regardless of which CSV types arrive in the batch.

Legacy `clusters`/`workloads` tables continue to coexist with the native schema during migration; upsert targets the native cluster model used by recommendation and staleness logic.

## Alternatives Considered

### Upsert only when processing succeeds

Downstream file tracking and hooks fail mid-batch if cluster row missing on partial failure paths.

### Lazy create on first CSV type match

Race when parallel workers process different files for the same cluster in the same manifest window.

### Drop legacy tables before upsert unify

Breaking change for deployments still reading legacy paths; out of scope for ingest ordering.

## Consequences

- `clusters.last_reported_at` advances on every payload, enabling staleness override in [ADR-0224](0224-stale-marking-precedence-last-reported-at-overrides-digest-age.md).
- Failed file processing still updates liveness — operators must use digest age and recommendation staleness filters, not assume failed ingest means stale cluster.
- Extra write per Kafka message; acceptable cost for correctness.

## Related Decisions

- [ADR-0089](0089-manual-kafka-commit-after-success.md): Manual Kafka commit after success.
- [ADR-0166](0166-report-file-status-manifest-gating.md): Per-file report status and manifest gating.
- [ADR-0224](0224-stale-marking-precedence-last-reported-at-overrides-digest-age.md): Staleness precedence uses `last_reported_at`.

## References

- [internal/processor/report_processor.go](../../internal/processor/report_processor.go)
- [internal/model/cluster.go](../../internal/model/cluster.go)
