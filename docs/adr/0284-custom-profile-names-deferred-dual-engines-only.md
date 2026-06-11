# ADR-0284: Custom profile names deferred — v1 limited to dual cost/performance engines

## Status

Accepted

## Phase

3+

## Context

Kruize offered `/listPerformanceProfiles` with named profiles (conservative, balanced, aggressive). Requirements included custom profile CRUD for user-defined percentile targets.

`recommendation_profiles` table exists but is unused ([ADR-0258](0258-recommendation-profiles-seeded-unused-percentiles-in-settings.md)).

## Decision

Defer custom profiles. V1 exposes two fixed engines (cost: P60 targets, performance: P98 targets) configured via settings thresholds (not profile objects). Custom profiles would require: profile CRUD API, engine parameterization, UI profile selector.

## Alternatives Considered

### Ship profiles now

Significant UI/API/engine work for unvalidated demand.

### Rename engines to profiles

Confusing nomenclature; engines and profiles are different concepts.

## Consequences

- Users configure via percentile thresholds in settings (less intuitive than named profiles).
- Two engines cover majority use case.
- Future profile system would sit above settings layer.

## Related Decisions

- [ADR-0258](0258-recommendation-profiles-seeded-unused-percentiles-in-settings.md): Dormant profiles table.
- [ADR-0004](0004-dual-cost-performance-engine-rows.md): Dual engines.
- [ADR-0261](0261-three-terms-short-medium-long-kruize-aligned-defaults.md): Three terms.

## References

- [internal/api/handlers_threshold_settings.go](../../internal/api/handlers_threshold_settings.go)
- [migrations/000026_recommendation_profiles.up.sql](../../migrations/000026_recommendation_profiles.up.sql)
