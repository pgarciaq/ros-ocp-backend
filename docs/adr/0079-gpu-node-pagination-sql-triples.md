# ADR-0079: Push GPU/node pagination into SQL triple expansion

## Status

Accepted

## Context

Loading all clusters in memory for time-slicing pagination doesn't scale.

## Decision

SQL-level pagination using three queries per page: COUNT for total, keys query for `(cluster, node, device)` triples, then detail fetch—LIMIT/OFFSET pushed into the keys query. In-memory pagination OOMs at 1000+ clusters (findings #48/#49).

## Alternatives Considered

### In-memory pagination after full cluster load
Loads all GPU/node triples into the API process; memory grows linearly with cluster count and caused OOM kills in large-tenant tests.

### Cursor-only pagination without total count
UI pagination controls require total row count; cursor alone forces "unknown total" UX that breaks PatternFly paginator components.

## Consequences

Memory-bounded. Consistent page sizes. More complex SQL.

## References

- [internal/engine/node_gpu_triples.go](internal/engine/node_gpu_triples.go)
