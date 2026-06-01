# Node recommendations

Node CPU/memory utilization recommendations identify underutilized,
overcommitted, and stranded worker capacity, with optional fleet consolidation
hints when multiple nodes share the same instance type.

## API

Canonical endpoint: `GET /api/cost-management/v1/recommendations/openshift/nodes`

- One object per node with nested `recommendation_terms` and per-engine sizing
- `filter[idle_state]=idle|zombie|active` for decommissioning workflows
- `filter[is_underutilized]`, `filter[cluster]`, `engine=cost|performance`
- `instance_type` on each row when the operator supplies it in ROS metrics

See [UI integration — node recommendations](../ui-integration-guide.md#3-node-recommendations)
and [validating native engine — node recommendations](../testing/validating-native-engine.md#node-recommendations-validation).

## Engine behavior

- **Classification:** Underutilized, overcommitted, stranded resources, trend slope
- **Idle state:** `active`, `idle`, or `zombie` (notification code **15**)
- **Consolidation:** `node_count_reduction` per engine/term; Level 3 groups by
  `instance_type` when present, otherwise per-node binary hints for underutilized nodes
- **Dual engines:** `cost` (higher target utilization) and `performance` (more headroom)

## Roadmap / deferred

| Item | Rationale |
|------|-----------|
| **Business hours for nodes** | Intentionally skipped. Nodes are always-on; `idle_state` covers the important decommissioning signal without schedule complexity. |
| **Tier 2 — MachineSet** | Future work. Requires metrics-operator MachineSet label collection, ingest/schema changes, engine grouping, API filters, and UI. |
| **Tier 3 — MachineAutoscaler** | Future work after Tier 2. Autoscaler-aware consolidation and scale-down guidance. |

Implementation references: [`RecommendNodes()`](../../internal/engine/recommend_nodes.go),
[`PersistNodeRecommendations()`](../../internal/engine/recommend_nodes.go),
[`GetNodeUtilizationRecs`](../../internal/api/handlers_node_utilization.go).
