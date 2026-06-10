# ADR-0031: Use snapshot priority-ordered rules (orphan > managed > redundant > stale > never-restored)

## Status

Accepted

## Context

Single-age heuristic can't encode FinOps policy for different snapshot states.

## Decision

Priority-ordered classification: orphaned (source PVC deleted), managed (recent + restored), redundant (multiple snapshots same PVC), stale (old), never-restored.

## Consequences

Explicit policy per state. More complex classification. Clear delete/keep guidance per category.

## References

- [internal/engine/snapshot_classify.go](internal/engine/snapshot_classify.go)
