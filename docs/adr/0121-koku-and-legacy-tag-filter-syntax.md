# ADR-0121: Support Koku-style filter[tag:key] and legacy tag=key:value

## Status

Accepted

## Context

Koku UI sends `filter[tag:key]=value`; legacy clients use `tag=key:value`.

## Decision

Accept both formats; normalize internally.

## Consequences

Backward compatible. No UI changes needed. Parser handles both.

## References

- [docs/features/tag-filtering.md](docs/features/tag-filtering.md)
