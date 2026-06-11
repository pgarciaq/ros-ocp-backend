# ADR-0239: Feature toggles vs plugin toggles distinction

## Status

Accepted

## Context

The plugin system ([ADR-0099](0099-compile-time-in-process-plugins.md)) registers CSV ingestors, produce phases, and API routes at compile time. [ADR-0157](0157-ros-enabled-plugins-replaces-native-flag.md) and [ADR-0158](0158-enabled-or-disabled-plugins-env.md) control **which plugins load** via `ROS_ENABLED_PLUGINS` / `ROS_DISABLED_PLUGINS`.

Separate boolean flags gate **behavior within** loaded plugins: business hours (`ROS_BUSINESS_HOURS_ENABLED`), savings estimates (`ROS_SAVINGS_ESTIMATES_ENABLED`), threshold recalculation (`ROS_THRESHOLD_RECALCULATION_ENABLED`), VM recommendations (`ROS_ENABLE_VM_RECS`, [ADR-0109](0109-vm-plugin-feature-gate.md)). Conflating these levels causes confusion when routes exist but behavior is no-op, or when plugins are disabled but flags remain set.

## Decision

Two distinct toggle layers:

| Layer | Mechanism | Effect |
|-------|-----------|--------|
| **Plugin toggle** | `ROS_ENABLED_PLUGINS` / `ROS_DISABLED_PLUGINS` | Registers or skips plugin `init()` paths: CSV types, produce hooks, API route registration |
| **Feature toggle** | Per-feature env booleans | Alters behavior inside enabled plugins; routes may remain mounted |

Disabling a **plugin** removes its API routes and CSV claim ([ADR-0168](0168-disabled-plugin-route-guards.md)). Disabling a **feature flag** keeps routes but changes computation (e.g., savings fields zeroed, BH digest stream skipped).

## Alternatives Considered

### Single ROS_ENABLED_* namespace for everything

Cannot disable VM plugin without distinct flag from VM sub-features; poor granularity.

### Feature flags as plugin names only

Cannot kill-switch savings globally across all resource types without disabling each plugin.

### Runtime plugin unload without restart

Not supported for compile-time plugins; env change requires process restart.

## Consequences

- Helm values must distinguish plugin lists from feature booleans.
- Capabilities endpoint ([ADR-0083](0083-capabilities-endpoint-locked-settings.md)) should reflect both disabled plugins and disabled features where UI-visible.
- Documentation cross-links plugin reference pages with feature flag tables in `configuration.md`.

## Related Decisions

- [ADR-0099](0099-compile-time-in-process-plugins.md): Compile-time in-process plugins.
- [ADR-0168](0168-disabled-plugin-route-guards.md): Disabled plugin route guards.
- [ADR-0160](0160-savings-estimates-kill-switch.md): Savings estimates kill-switch.

## References

- [internal/config/config.go](../../internal/config/config.go)
- [internal/plugins/registry.go](../../internal/plugins/registry.go)
