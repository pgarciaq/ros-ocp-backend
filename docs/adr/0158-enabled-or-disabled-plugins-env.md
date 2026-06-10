# ADR-0158: Use ROS_ENABLED_PLUGINS allowlist OR ROS_DISABLED_PLUGINS blocklist

## Status

Accepted

## Context

Rebuilding binary per deployment variant is too expensive.

## Decision

Runtime enable/disable via environment variables. Both allowlist and blocklist supported (not both simultaneously).

## Consequences

Flexible deployment configuration. Must validate at startup. One of the two must be used, not both.

## References

- [docs/architecture/plugin-architecture.md](docs/architecture/plugin-architecture.md)
