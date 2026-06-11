# ADR-0031: Use snapshot priority-ordered rules (orphan > managed > redundant > stale > never-restored)

## Status

Accepted

## Context

Single-age heuristic can't encode FinOps policy for different snapshot states.

## Decision

Priority-ordered classification: orphaned (source PVC deleted), managed (recent + restored), redundant (multiple snapshots same PVC), stale (old), never-restored.

## Alternatives Considered

### Single age heuristic
Cannot distinguish orphaned snapshots (safe to delete) from stale-but-restored backups (may still be needed). Age alone mis-ranks redundant copies of the same PVC.

### Weighted score combining dimensions
Opaque numeric score frustrates operators auditing why a snapshot is "redundant" vs "stale"; support tickets increase when policy isn't explainable.

### Per-rule priority without global ordering
Ambiguous tie-breaking when a snapshot matches multiple rules (e.g. stale and redundant); inconsistent delete/keep guidance across runs.

## Consequences

Explicit policy per state. More complex classification. Clear delete/keep guidance per category.

## References

- [internal/engine/snapshot_classify.go](internal/engine/snapshot_classify.go)
