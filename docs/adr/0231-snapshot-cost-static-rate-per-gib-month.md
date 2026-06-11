# ADR-0231: Snapshot cost uses static $/GiB/month (not Koku effective rates)

## Status

Accepted

## Context

Volume snapshot recommendations need dollar savings to rank delete candidates. Container and node savings derive rates from Koku Masu `effective_rates` (see [ADR-0111](0111-rates-from-koku-masu.md)). Snapshots are a different billing surface: restore size in GiB × storage-month rate, not CPU/memory namespace aggregates.

[ADR-0116](0116-snapshot-cost-fallback-chain.md) documented a fallback chain including Masu rates. In practice, snapshot savings do not track cost model changes in real time — operators need a predictable, tenant-configurable static rate for FinOps planning.

## Decision

Snapshot savings use a tenant-configurable `cost_per_gib_month` setting (Settings API / env), not live Koku effective rates. When unset, a compiled static default applies. Snapshot cost does not invalidate or refresh when Masu rates change unless the operator updates the snapshot settings domain.

This supersedes the Masu-dependent portions of [ADR-0116](0116-snapshot-cost-fallback-chain.md) for the primary savings path.

## Alternatives Considered

### Masu effective_rates as primary source

Couples snapshot dollars to namespace storage aggregates that may not reflect snapshot billing. Cost model churn causes savings swings unrelated to snapshot inventory changes.

### Hybrid: Masu with static fallback only on outage

Still ties nominal savings to upstream rate volatility; harder to explain to storage teams.

### No dollar savings on snapshots

Incomplete FinOps picture; operators cannot prioritize delete candidates without impact estimates.

## Consequences

- Snapshot savings remain stable across cost model updates until settings change.
- Operators must configure `cost_per_gib_month` to match their storage tariff for accurate dollars.
- Container/node rate cache ([ADR-0228](0228-effective-rates-cache-key-org-cluster-only.md)) does not apply to snapshot cost.
- Documentation and Settings API must clearly label snapshot cost as static, not Masu-derived.

## Related Decisions

- [ADR-0230](0230-snapshot-inventory-append-only-freshness-window.md): Snapshot inventory freshness for classification.
- [ADR-0228](0228-effective-rates-cache-key-org-cluster-only.md): Effective rates cache (containers/nodes, not snapshots).
- [ADR-0116](0116-snapshot-cost-fallback-chain.md): Prior fallback chain — partially superseded.

## References

- [internal/engine/snapshot_settings.go](../../internal/engine/snapshot_settings.go)
- [internal/plugins/snapshot/produce.go](../../internal/plugins/snapshot/produce.go)
