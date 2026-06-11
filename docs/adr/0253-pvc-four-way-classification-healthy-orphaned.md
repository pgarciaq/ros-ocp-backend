# ADR-0253: PVC four-way classification including healthy and orphaned

## Status

Accepted

## Context

PVC recommendations ([ADR-0025](0025-pvc-thresholds-20-oversized-85-near-full.md), [ADR-0026](0026-pvc-size-max-usage-times-2-floor-1gib.md)) previously emphasized oversized and near-full states. List APIs and fleet counts expect a row per tracked PVC; operators also need explicit **healthy** classification and **orphaned** detection when usage flatlines at zero.

Notification code 20 identifies orphaned PVC candidates for cleanup workflows.

## Decision

PVC classification uses **four mutually exclusive categories**:

| Class | Criteria (simplified) |
|-------|------------------------|
| `orphaned` | All-zero usage for ≥ min observation days |
| `oversized` | Usage ≪ capacity with stable downward/flat trend ([ADR-0025](0025-pvc-thresholds-20-oversized-85-near-full.md)) |
| `near_full` | Utilization above near-full threshold |
| `healthy` | Within bounds — no resize action |

**Orphaned** emits notification **20**. **Healthy** still produces a recommendation row with **no sizing action** — preserves completeness for list `meta.count` and UI filters.

## Alternatives Considered

### Omit healthy rows from DB

Under-counts PVC inventory in fleet summaries.

### Merge orphaned into oversized

Different operator action (delete vs shrink); notification codes differ.

### Orphaned as notification-only, no row

Breaks list/detail parity for orphaned PVCs.

## Consequences

- Storage for healthy rows trades space for consistent pagination ([ADR-0188](0188-list-query-keys-pagination-refilter-detail.md)).
- Orphaned min-days gate prevents false positives on new PVCs.
- Near-full path may include days-to-full projection ([ADR-0254](0254-pvc-growth-projection-decay-weighted-slope.md)).

## Related Decisions

- [ADR-0025](0025-pvc-thresholds-20-oversized-85-near-full.md): PVC thresholds.
- [ADR-0026](0026-pvc-size-max-usage-times-2-floor-1gib.md): Recommended size formula.
- [ADR-0254](0254-pvc-growth-projection-decay-weighted-slope.md): Growth projection.

## References

- [internal/plugins/pvc/classify.go](../../internal/plugins/pvc/classify.go)
- [internal/engine/notification_codes.go](../../internal/engine/notification_codes.go)
