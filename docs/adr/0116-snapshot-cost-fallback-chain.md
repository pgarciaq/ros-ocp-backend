# ADR-0116: Use snapshot cost chain: Settings → env → effective_rates → $0.05 default

## Status

Accepted

## Context

PVC storage rate is a reasonable proxy for snapshot cost; no direct billing integration yet.

## Decision

Cascading fallback chain for snapshot cost per GiB: live Masu rate → cached rate → last-known rate → zero default.

## Alternatives Considered

### Single cost source (Masu only)
Breaks snapshot savings entirely when Masu is down; snapshots are long-lived assets that need cost even during transient outages.

### No cost on snapshots
Incomplete FinOps picture; storage teams cannot prioritize delete candidates without dollar impact.

## Consequences

Reasonable default when upstream rates unavailable. Ops can override via settings chain. Replaced the prior COST-7523 single-default approach with explicit fallback ordering.

## Migration Notes

Prior to this ADR, snapshot cost used a fixed default ($0.05/GiB) when Masu was unavailable (COST-7523 interim). Deployments should expect the new chain: Settings → env → effective_rates → $0.05 default. No schema migration; behavior change only on next savings recompute.

## References

- [internal/engine/snapshot_settings.go](internal/engine/snapshot_settings.go)
