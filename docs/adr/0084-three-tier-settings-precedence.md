# ADR-0084: Use three-tier settings precedence: env lock → DB → default

## Status

Accepted

## Context

DB-only overrides that SaaS ops couldn't enforce globally.

## Decision

Environment variable locks override tenant DB settings which override compiled defaults. Within DB settings, namespace overrides beat cluster overrides beat org-wide defaults.

## Alternatives Considered

### Single global settings only
No per-namespace tuning for teams with different idle thresholds or consolidation appetite within the same org.

### Two-tier only (org + namespace, no cluster level)
Missing cluster-scoped overrides for multi-cluster tenants where one cluster runs batch workloads and another runs latency-sensitive services.

## Related Decisions

- [ADR-0083](0083-capabilities-endpoint-locked-settings.md): capabilities endpoint exposes which settings env locks have frozen.
- Precedence chain: env lock → DB (namespace > cluster > org) → compiled default.

## Consequences

Ops can force-lock settings. Tenants can customize within bounds. Clear precedence chain.

## References

- [docs/architecture/configurability.md](docs/architecture/configurability.md)
- [internal/engine/threshold_settings.go](internal/engine/threshold_settings.go)

## Status Update (2026-06)

The three-tier precedence (env lock → DB → defaults) is accurate, but the DB tier is **org-wide only** for all domains except business hours. There are no `cluster_uuid` or `namespace` columns on `recommendation_thresholds`. See [ADR-0208](0208-settings-scope-org-wide-only-except-business-hours.md) for the authoritative scope documentation. Business hours uses hierarchical scoping via a separate table (`business_hour_schedules`).
