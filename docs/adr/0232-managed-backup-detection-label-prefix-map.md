# ADR-0232: Managed backup detection via label prefix map

## Status

Accepted

## Context

Snapshot classification in [ADR-0031](0031-snapshot-priority-ordered-rules.md) uses priority-ordered rules: orphan > managed > redundant > stale > never-restored. The `managed` category must identify snapshots created by enterprise backup tools (Velero, Kasten, Trilio, etc.) so they are not recommended for deletion alongside orphaned or stale copies.

Operators apply vendor-specific labels to backup snapshots. A hardcoded list of exact label keys does not scale across backup product versions and custom integrations.

## Decision

Maintain a label **prefix map** in code: known backup vendor label prefixes map to the `managed` classification. When any snapshot label key matches a configured prefix, `managed` wins in the priority chain and supersedes `redundant` and `stale` for that snapshot.

Custom backup labels that do not match any known prefix classify as `non-managed` and remain eligible for redundant/stale/orphan rules.

## Alternatives Considered

### Exact label key allowlist

Breaks on vendor renames and minor label schema changes; high maintenance.

### Annotation-only detection

Many operators use labels, not annotations, for backup tooling metadata.

### User-configurable prefix map in DB

Adds settings surface for rarely changed vendor list; deferred to env/code updates.

## Consequences

- Backup vendor additions require a code change and ADR note when prefixes are added.
- Mislabeled snapshots (no matching prefix) may receive delete recommendations despite being backups — operators must apply known prefixes or accept classification as non-managed.
- Priority ordering from [ADR-0031](0031-snapshot-priority-ordered-rules.md) remains authoritative; this ADR only defines how `managed` is detected.

## Related Decisions

- [ADR-0031](0031-snapshot-priority-ordered-rules.md): Snapshot priority-ordered classification rules.
- [ADR-0230](0230-snapshot-inventory-append-only-freshness-window.md): Inventory freshness window.

## References

- [internal/engine/snapshot_classify.go](../../internal/engine/snapshot_classify.go)
- [internal/plugins/snapshot/backup_labels.go](../../internal/plugins/snapshot/backup_labels.go)
