# ADR-0032: Use restoreSize for snapshot cost, not CSI byte metrics

## Status

Accepted

## Context

Per-driver storage scraping is non-portable; actual billed size varies by CSI driver.

## Decision

Use `restoreSize` from VolumeSnapshot as cost proxy. Defer billing-accurate costs to COST-7523.

## Consequences

Approximate cost. Portable across CSI drivers. May under/over-estimate actual billing.

## References

- [docs/architecture/cost-integration.md](docs/architecture/cost-integration.md)
- [internal/engine/snapshot_settings.go](internal/engine/snapshot_settings.go)
