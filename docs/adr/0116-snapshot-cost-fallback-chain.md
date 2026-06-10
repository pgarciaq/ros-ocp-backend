# ADR-0116: Use snapshot cost chain: Settings → env → effective_rates → $0.05 default

## Status

Accepted

## Context

PVC storage rate is a reasonable proxy for snapshot cost; no direct billing integration yet.

## Decision

Cascading fallback chain for snapshot cost per GiB.

## Consequences

Reasonable default. Ops can override. Will be replaced by COST-7523 billing-accurate costs.

## References

- [internal/engine/snapshot_settings.go](internal/engine/snapshot_settings.go)
