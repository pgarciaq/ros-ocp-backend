# ADR-0279: Namespace recommendations: Unleash kill-switch then default-on

## Status

Accepted

## Phase

6

## Context

Namespace-level recommendations were new and potentially noisy during initial launch. Unleash feature flag provided per-org rollout control while validating engine behavior in production.

After validation period, maintaining Unleash dependency for a stable feature added operational complexity.

## Decision

Initially gated behind Unleash flag (per-org enablement). After validation, removed flag and made default-on via plugin system ([ADR-0158](0158-plugin-toggles-over-unleash-for-plugins.md)). Transition: Unleash flag → `ROS_ENABLED_PLUGINS` includes namespace → default enabled.

## Alternatives Considered

### Keep Unleash permanently

Dependency + complexity for stable feature.

### Ship without flag

Risky for new recommendation class without production validation.

## Consequences

- Feature flag code removed (no maintenance burden).
- Plugin toggle provides deployment-level control.
- Per-org granularity lost (acceptable — namespace recs are stable).

## Related Decisions

- [ADR-0158](0158-plugin-toggles-over-unleash-for-plugins.md): Plugin toggles over Unleash.
- [ADR-0239](0239-feature-toggles-vs-plugin-toggles.md): Feature vs plugin toggles.
- [ADR-0014](0014-namespace-idle-after-container-gpu-priority-90.md): Namespace idle priority.

## References

- [internal/plugins/namespace/namespace.go](../../internal/plugins/namespace/namespace.go)
- [internal/plugins/registry.go](../../internal/plugins/registry.go)
