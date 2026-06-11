# ADR-0258: recommendation_profiles table seeded but unused — percentiles live in settings API

## Status

Accepted

## Phase

1–3

## Context

Migration 000026 seeds `recommendation_profiles` with cost/performance percentile configurations intended for Kruize parity. Each row defines named profile targets with CPU and memory percentile values.

However, the native engine never reads this table. Percentile targets for cost and performance engines are resolved via the settings three-tier precedence ([ADR-0084](0084-three-tier-settings-precedence.md)): environment lock → `recommendation_thresholds` DB rows → compile-time defaults in Go.

Engine code paths query `recommendation_thresholds`, not `recommendation_profiles`. The seeded profiles table is functionally dead in native mode.

## Decision

`recommendation_profiles` remains as a seeded reference table but is not consumed by any Go code in the native engine. Percentile targets for cost/performance engines are resolved exclusively via the settings three-tier precedence (env lock → DB thresholds → defaults).

The profiles table may be removed in a future cleanup migration once Kruize plugin deprecation is complete and no external consumer reads it.

Custom named profiles (conservative, balanced, aggressive, user-defined CRUD) are **deferred for v1** (incorporates former ADR-0284). V1 exposes two fixed engines (cost: P60 targets, performance: P98 targets) configured via settings percentile thresholds — not profile objects. A future profile system would sit above the settings layer and require profile CRUD API, engine parameterization, and UI profile selector work that was not validated for initial delivery.

## Alternatives Considered

### Use profiles as the authoritative source

Conflicts with the settings API already shipped and documented. Would require migrating threshold configuration into profile objects.

### Remove immediately in phase 1

Risks breaking the Kruize plugin if it still reads profiles during rollback scenarios.

### Dual-read (profiles fallback when thresholds missing)

Adds resolution complexity without clear benefit — thresholds table covers all cases.

### Ship custom profiles now (Kruize `/listPerformanceProfiles` parity)

Significant UI/API/engine work for unvalidated demand; deferred in favor of dual-engine + settings thresholds.

### Rename engines to profiles

Confusing nomenclature; engines and profiles are different concepts.

## Consequences

- Engineers may assume profiles drive engine behavior; they do not.
- Settings API is the authoritative source for percentile configuration.
- Table exists for backward-compatible schema but receives no reads or writes from native code.
- Future cleanup requires verifying zero Kruize consumers before DROP.

## Related Decisions

- [ADR-0004](0004-dual-cost-performance-engine-rows.md): Dual cost/performance engine rows.
- [ADR-0006](0006-p60-vs-p98-cpu-p95-vs-max-memory.md): P60 vs P98 percentile targets.
- [ADR-0208](0208-settings-scope-org-wide-only-except-business-hours.md): Settings scope org-wide only.
- [ADR-0261](0261-three-terms-short-medium-long-kruize-aligned-defaults.md): Three terms.

## References

- [migrations/000026_recommendation_profiles.up.sql](../../migrations/000026_recommendation_profiles.up.sql)
- [internal/api/handlers_threshold_settings.go](../../internal/api/handlers_threshold_settings.go)
