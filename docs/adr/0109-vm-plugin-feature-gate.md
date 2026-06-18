# ADR-0109: Gate VM plugin with ROS_ENABLE_VM_RECS

## Status

Superseded — VM plugin is now controlled exclusively by `ROS_ENABLED_PLUGINS` /
`ROS_DISABLED_PLUGINS` (see ADR-0239). The `ROS_ENABLE_VM_RECS` environment
variable has been removed.

## Context

VM support depends on operator CSV maturity not yet GA.

## Decision

VM plugin behind explicit feature gate until operator stabilizes.

## Superseded By

ADR-0239 established a unified two-layer toggle architecture:
- **Plugin toggles** (`ROS_ENABLED_PLUGINS`, `ROS_DISABLED_PLUGINS`) control
  plugin loading, route registration, and CSV processing.
- **Feature toggles** control cross-cutting behavior within enabled plugins.

The VM plugin is now enabled by default (included in the plugin registry) and
can be disabled via `ROS_DISABLED_PLUGINS=vm`. The separate `ROS_ENABLE_VM_RECS`
boolean was redundant and has been removed to reduce configuration surface area
and avoid the dual-check confusion.

## Consequences

No VM processing unless opted in. Safe default. Can enable per-cluster.

## References

- [internal/plugins/vm/plugin.go](internal/plugins/vm/plugin.go)
- [ADR-0239: Feature toggles vs plugin toggles](0239-feature-toggles-vs-plugin-toggles.md)
