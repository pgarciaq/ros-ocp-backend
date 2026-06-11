# ADR-0201: Cluster ingest_hooks_failed degradation flag

## Status

Accepted

## Context

Ingest hooks (tag sync, analytics triggers) can fail independently of core ingestion ([ADR-0102](0102-ingest-hook-failures-non-fatal.md)). The system needs to distinguish "data ingested but hooks failed" from "analytics incomplete" from "manifest not complete."

## Decision

Add cluster-level degradation flags:

- `clusters.ingest_hooks_failed` (boolean)
- `clusters.ingest_hooks_failed_at` (timestamp)

Set when any ingest hook returns error. Cleared on next successful ingest with hooks passing. Surfaced in API detail responses.

**Non-fatal:** Core recommendations still produced from ingested data.

## Consequences

Three distinct degradation states:

1. **Manifest incomplete** — no recommendations
2. **Hooks failed** — recommendations exist, enrichment partial
3. **Analytics incomplete** — recommendations exist, quality missing ([ADR-0062](0062-analytics-incomplete-flag-on-failure.md))

Operators can triage which subsystem needs attention.

## Alternatives Considered

### Single "unhealthy" flag

Loses diagnostic granularity. Rejected.

### Separate hook-level flags

Too many fields for the current hook count. Rejected.

## Related Decisions

- [ADR-0102](0102-ingest-hook-failures-non-fatal.md): Ingest hook failure semantics.
- [ADR-0180](0180-analytics-write-ordering-strict-mode.md): Analytics write ordering.

## References

- [internal/engine/cluster_ingest_hooks.go](../../internal/engine/cluster_ingest_hooks.go)
- [migrations/000142_cluster_ingest_hooks_failed.up.sql](../../migrations/000142_cluster_ingest_hooks_failed.up.sql)
