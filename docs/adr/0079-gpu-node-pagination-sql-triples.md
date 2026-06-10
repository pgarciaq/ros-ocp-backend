# ADR-0079: Push GPU/node pagination into SQL triple expansion

## Status

Accepted

## Context

Loading all clusters in memory for time-slicing pagination doesn't scale.

## Decision

SQL-level pagination of node/GPU "triples" (cluster, node, device) with LIMIT/OFFSET pushed into query.

## Consequences

Memory-bounded. Consistent page sizes. More complex SQL.

## References

- [internal/engine/node_gpu_triples.go](internal/engine/node_gpu_triples.go)
