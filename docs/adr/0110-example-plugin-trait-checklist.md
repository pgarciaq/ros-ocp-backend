# ADR-0110: Use _example plugin as compile-time trait checklist

## Status

Accepted

## Context

Documentation-only templates don't prove interfaces compile together.

## Decision

Maintain `_example` plugin that implements all traits as reference and compile check.

## Consequences

Always-compiling reference. New developers can copy. CI catches interface drift.

## References

- [internal/plugins/example/plugin.go](internal/plugins/example/plugin.go)
