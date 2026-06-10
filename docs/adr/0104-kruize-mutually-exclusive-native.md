# ADR-0104: Make Kruize mutually exclusive with native plugins

## Status

Accepted

## Context

Dual-engine runs would duplicate recommendations and savings.

## Decision

`ROS_ENABLED_PLUGINS=kruize` disables all native plugins and vice versa.

## Consequences

Clean migration path. No accidental dual-write. Configuration validates at startup.

## References

- [internal/plugin/registry.go](internal/plugin/registry.go)
