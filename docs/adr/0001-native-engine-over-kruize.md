# ADR-0001: Use native Go engine over Kruize for production recommendations

## Status

Accepted

## Context

Kruize was an external Java service with JSONB-heavy storage, limited notification/savings coverage, and tight coupling via HTTP + Kafka recommendation topics.

## Decision

Build an in-process Go engine that writes relational columns; Kruize becomes optional legacy plugin (`ROS_ENABLED_PLUGINS=kruize`), mutually exclusive with native plugins.

## Alternatives Considered

### Continue Kruize as primary engine with incremental fixes
Patching Kruize's JSONB schema and HTTP API would preserve the existing Autotune investment but could not deliver dollar savings, business-hours dual streams, VM/quota plugins, or the 54+ notification codes the UI expects—all of which require relational columns and in-process enrichment that Kruize never implemented.

### Sidecar microservice (gRPC) instead of in-process engine
A separate Go recommendation service would isolate failures and allow independent scaling, but would reintroduce network latency on every ingest hook and API enrichment pass, duplicate deployment artifacts in cost-onprem, and complicate FIPS-compliant image builds already strained on aarch64 SNO clusters.

### Dual-write to both Kruize and native during migration
Running both engines in parallel would simplify A/B validation, but doubles Kafka consumer load, risks divergent recommendations confusing the UI, and leaves JSONB storage overhead until Kruize is fully retired—maintenance burden with no long-term upside once native parity was proven.

## Consequences

Eliminated Java dependency, JSONB overhead, HTTP latency. Requires maintaining engine math in-house. Kruize path retained for rollback.

## References

- [docs/architecture/native-migration.md](docs/architecture/native-migration.md)
- [internal/engine/recommend_all.go](internal/engine/recommend_all.go)
