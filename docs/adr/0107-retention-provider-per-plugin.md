# ADR-0107: Use RetentionProvider per plugin with core fallback slice

## Status

Accepted

## Context

Single global retention table list would miss plugin-specific tables when plugins disabled.

## Decision

Each plugin declares its retention tables; core provides fallback for always-on tables.

## Consequences

Correct retention regardless of plugin config. Plugins own their data lifecycle.

## References

- [internal/engine/retention.go](internal/engine/retention.go)
