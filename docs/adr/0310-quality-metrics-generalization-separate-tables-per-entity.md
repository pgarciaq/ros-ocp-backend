# ADR-0310: Quality metrics generalization — separate tables per entity type

## Status

Accepted

## Supersedes

[ADR-0275](0275-quality-metrics-container-only-internal-not-primary-ui.md) (quality metrics container-only and internal)

## Phase

17

## Context

Quality metrics were initially limited to containers only (ADR-0275). The original decision was based on:

1. Different signal characteristics across entity types (continuous sizing vs categorical recommendations).
2. Desire to keep quality internal/diagnostic rather than a primary UI surface.
3. Uncertainty about what stability/adoption means for non-container resources.

Since then, the system has expanded to support PVC, VM, GPU MIG, and snapshot recommendations, each with well-defined quality signals. Operators need quality visibility across all entity types to:

- Detect flip-flopping recommendations (stability) across all resource types.
- Track whether recommendations are being applied (adoption) fleet-wide.
- Identify adverse outcomes specific to each entity (OOM for containers, near-full for PVCs, saturation for VMs, contention for GPU MIG).

This ADR generalizes quality metrics to PVC, VM, GPU MIG, and snapshot entity types, with node and GPU timeslicing deferred pending operator changes.

## Decision

**Option B — separate tables per entity type.**

Each entity type gets its own quality table with entity-appropriate primary key and columns:

| Entity | Table | PK includes | Entity-specific columns |
|--------|-------|-------------|------------------------|
| Container | `recommendation_quality` | org_id, cluster_uuid, namespace, workload_name, container_name, term, engine, measured_at | `oom_events_after_rec` |
| PVC | `pvc_recommendation_quality` | org_id, cluster_uuid, namespace, pvc_name, term, engine, measured_at | `days_above_threshold` |
| VM | `vm_recommendation_quality` | org_id, cluster_uuid, namespace, vm_name, term, engine, measured_at | `saturation_days` |
| GPU MIG | `gpu_mig_recommendation_quality` | org_id, cluster_uuid, namespace, workload_name, container_name, term, engine, measured_at | `contention_days` |
| Snapshot | `snapshot_recommendation_quality` | org_id, cluster_uuid, namespace, snapshot_name, measured_at | (none — adoption only) |

All tables share common columns: `stability_pct`, `adoption_detected`, `recommendation_age_hours`, `measured_at`.

API endpoints:

- `GET /recommendations/openshift/quality/containers` (alias: `/quality`)
- `GET /recommendations/openshift/quality/pvcs`
- `GET /recommendations/openshift/quality/vms`
- `GET /recommendations/openshift/quality/gpu`
- `GET /recommendations/openshift/quality/snapshots`

## Rationale

1. **Matches existing architecture.** Recommendation sets are already separate per entity type (`recommendation_sets`, `pvc_recommendation_sets`, `vm_recommendations`, `gpu_mig_recommendation_sets`, `snapshot_recommendations`). Quality tables mirror this pattern.

2. **Zero migration risk to containers.** The existing `recommendation_quality` table is unchanged. New tables are additive.

3. **Clean indexes.** Each table's PK and indexes are optimized for that entity's access patterns without nullable columns or compound entity-type discriminators.

4. **Independent evolution.** Each entity can add columns (e.g., GPU might add `memory_bandwidth_days` later) without schema changes affecting other entities.

5. **Partition strategy.** All quality tables partition by `measured_at` month, consistent with the existing `recommendation_quality` pattern. Retention is unified under `ROS_HISTORY_RETENTION_DAYS`.

## Entity-Specific Quality Signals

| Signal | Containers | PVC | VM | GPU MIG | Snapshot |
|--------|-----------|-----|----|---------|---------:|
| **Stability** | Continuous (weighted CPU+mem variation) | Continuous (bytes variation) | Continuous (weighted vCPU+mem variation) | Binary (profile unchanged = 1.0, changed = 0.0) | N/A (null) |
| **Adoption** | Config match within 5% (CPU+memory requests) | Capacity match within 5% | vCPU+memory match within 5% | Profile exact match | Disappearance from inventory |
| **Bad outcome** | OOM events (`oom_events_after_rec`) | Usage ratio > 95% capacity (`days_above_threshold`) | CPU/mem > 95% allocated (`saturation_days`) | SM active exceeds profile capacity (`contention_days`) | N/A (none) |

## Rejected Alternatives

### Option A: Single unified table with nullable columns

A single `recommendation_quality_all` table with columns for every entity type (nullable where not applicable):

```sql
CREATE TABLE recommendation_quality_all (
    entity_type TEXT NOT NULL,  -- 'container', 'pvc', 'vm', 'gpu_mig', 'snapshot'
    -- entity identifiers (most nullable)
    container_name TEXT,
    pvc_name TEXT,
    vm_name TEXT,
    snapshot_name TEXT,
    -- outcome signals (all nullable)
    oom_events_after_rec INTEGER,
    days_above_threshold INTEGER,
    saturation_days INTEGER,
    contention_days INTEGER,
    ...
);
```

**Rejected because:**
- Column bloat: most columns null on any given row.
- Messy composite PK or surrogate key required.
- Index overhead: entity-specific queries need partial indexes.
- Schema coupling: adding GPU-specific columns requires altering a table used by all entities.

### Option C: Generalized entity_id with foreign key lookup

A single table with `entity_type` + `entity_id` (UUID) pointing to the respective recommendation set:

```sql
CREATE TABLE recommendation_quality_generic (
    entity_type TEXT NOT NULL,
    entity_id UUID NOT NULL,  -- FK to different tables depending on entity_type
    stability_pct NUMERIC,
    adoption_detected BOOLEAN,
    outcome_value INTEGER,  -- overloaded meaning per entity_type
    ...
);
```

**Rejected because:**
- Requires data migration for existing `recommendation_quality` rows.
- Foreign key cannot be enforced across multiple target tables without triggers.
- `outcome_value` column has different semantics per entity type — less ergonomic for querying and API serialization.
- Loses type safety and requires runtime interpretation of overloaded columns.

## Tier 3 Blockers (Not Yet Implemented)

| Entity | Blocker | Required Change |
|--------|---------|-----------------|
| Node | Node eviction events not reported by operator | Operator must expose `kube_node_evictions_total` or equivalent counter CSV |
| GPU Timeslicing | Current time-slicing configuration not reported | Operator must report active GPU time-slicing config for adoption detection |

## Consequences

- ADR-0275's constraint ("quality metrics are container-only") is superseded.
- Quality remains a diagnostic/power-user surface, not primary UI navigation (that aspect of ADR-0275 is preserved as a UX principle, not a technical limitation).
- Five new API endpoints are registered; the legacy `/quality` path remains as an alias for `/quality/containers`.
- Retention cleanup (`ROS_HISTORY_RETENTION_DAYS`) drops partitions from all quality tables uniformly.
- Pipeline resilience pattern (non-fatal quality writes) applies to all entity types.

## Related Decisions

- [ADR-0275](0275-quality-metrics-container-only-internal-not-primary-ui.md): Original container-only decision (superseded).
- [ADR-0179](0179-recommendation-quality-stability-formula.md): Stability score formula.
- [ADR-0181](0181-adoption-detection-all-term-engine-rows.md): Adoption detection logic.
- [ADR-0058](0058-partition-by-usage-start-month.md): Partition-by-month strategy.
- [ADR-0107](0107-retention-provider-per-plugin.md): Per-plugin retention providers.

## References

- `internal/engine/quality.go` — Container quality writer
- `internal/engine/pvc_quality.go` — PVC quality writer
- `internal/engine/vm_quality.go` — VM quality writer
- `internal/engine/gpu_quality.go` — GPU MIG quality writer
- `internal/engine/snapshot_quality.go` — Snapshot quality writer
- `internal/api/handlers_quality.go` — Quality API handlers
