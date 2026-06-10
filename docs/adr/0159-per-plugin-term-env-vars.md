# ADR-0159: Use per-plugin term env vars ROS_TERMS_<PLUGIN>_<TERM>_*

## Status

Accepted

## Context

Hardcoded global windows inappropriate for all tenants/plugins.

## Decision

Per-plugin, per-term configuration via structured env var naming.

## Consequences

Full term customization. Many env vars possible. Documented in configuration guide.

## References

- [internal/engine/term_config.go](internal/engine/term_config.go)
