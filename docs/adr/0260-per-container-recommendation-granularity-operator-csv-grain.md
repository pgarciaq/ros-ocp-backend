# ADR-0260: Per-container recommendation granularity matching operator CSV grain

## Status

Accepted

## Phase

1

## Context

Operator CSV (`ocp_ros_usage.csv`) reports metrics at `container_name` level within each workload. VPA operates at pod level. Deployment-level aggregation loses sidecar accuracy — a logging sidecar and main application container have vastly different resource profiles.

Early design discussions considered pod-level or deployment-level aggregation to reduce row counts.

## Decision

Recommendations are produced and stored per `(org_id, cluster_uuid, namespace, workload_type, workload, container_name)`. This matches the operator's reporting grain exactly. No pod-level or deployment-level aggregation in the engine.

## Alternatives Considered

### Per-pod aggregation

Loses sidecar accuracy; VPA already handles pod-level if needed separately.

### Per-deployment aggregation

Too coarse — averages across replicas and containers mask outliers.

### Per-container + deployment aggregation view

Considered for MachineSet Tier-2 ([ADR-0278](0278-machineset-tier2-engine-deferred-scope-criteria.md)).

## Consequences

- High row counts for multi-container pods (each container gets independent recommendation).
- Sidecar containers get appropriate sizing independent of main container.
- PK design flows from this choice ([ADR-0049](0049-term-engine-workload-type-in-pks.md)).
- VPA/HPA object-level recommendations deferred ([ADR-0276](0276-hpa-vpa-recommendations-deferred-advisory-only.md)).

## Related Decisions

- [ADR-0045](0045-daily-digest-tables-not-raw-metrics.md): Daily digest tables.
- [ADR-0170](0170-machineset-tier1-aggregation-over-node-recommendations.md): MachineSet Tier-1 aggregation.
- [ADR-0276](0276-hpa-vpa-recommendations-deferred-advisory-only.md): HPA/VPA deferral.
- [ADR-0278](0278-machineset-tier2-engine-deferred-scope-criteria.md): MachineSet Tier-2 deferred.

## References

- [internal/model/recommendation_sets.go](../../internal/model/recommendation_sets.go)
- [internal/services/csv_parser.go](../../internal/services/csv_parser.go)
