# ADR-0102: Treat IngestHook failures as non-fatal

## Status

Accepted

## Context

GPU digest upsert bug must not block container recommendations (primary product).

## Decision

Hook errors logged + counted but processing continues.

## Alternatives Considered

### Fail-closed on hook error
One auxiliary plugin failure (e.g. GPU digest upsert) blocks all container recommendations—the primary product value.

### Retry loop inside ingest pipeline
Delays entire ingest batch; poison-hook scenarios stall Kafka consumer lag unbounded.

## Consequences

Container reliability isolated from auxiliary plugins. Monitor `rosocp_ingest_hook_failures_total`; degradation surfaced via `ingest_hooks_failed` flag on ingest status responses.

## Cluster ingest_hooks_failed flag (migration 000142)

When any ingest hook fails during processing, the cluster row is updated:

- `clusters.ingest_hooks_failed` — boolean, set `true` on hook failure
- `clusters.ingest_hooks_failed_at` — timestamp of the failure

Cleared on the next successful ingest with all hooks passing (`internal/engine/cluster_ingest_hooks.go`).

API detail responses expose `ingest_hooks_failed` and `ingest_hooks_failed_at` on container list/detail payloads so operators can detect degraded auxiliary plugin state without blocking primary container recommendations.

Operational remediation is documented in [docs/operations/runbooks.md](../operations/runbooks.md) (ingest hook failure runbook): investigate `ros_ocp_plugin_hook_errors_total`, query affected clusters, re-trigger ingestion via Kafka replay or Koku `reship_ros`.

## References

- [docs/architecture/plugin-architecture.md](../architecture/plugin-architecture.md)
- [internal/engine/cluster_ingest_hooks.go](../../internal/engine/cluster_ingest_hooks.go)
- [migrations/000142_cluster_ingest_hooks_failed.up.sql](../../migrations/000142_cluster_ingest_hooks_failed.up.sql)
