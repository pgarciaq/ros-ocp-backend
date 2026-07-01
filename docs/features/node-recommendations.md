# Node Consolidation & Right-Sizing (Internal)

**Status:** Tier 1 shipped. Public feature guide: [Node recommendations](../../docs-site/features/node-recommendations.md).

Internal engineering reference for node utilization classification, dual-engine
consolidation, and the `/nodes` list API. GPU time-slicing is a separate plugin —
see [gpu-time-slicing.md](gpu-time-slicing.md) (`GET .../gpu/timeslicing`).

---

## Key implementation files

| Area | Path |
|------|------|
| Node plugin | [`internal/plugins/node/`](../../internal/plugins/node/) |
| Recommendation engine | [`internal/engine/recommend_nodes.go`](../../internal/engine/recommend_nodes.go) |
| Node savings | [`internal/engine/node_savings.go`](../../internal/engine/node_savings.go) |
| List / detail API | [`internal/api/handlers_node_utilization.go`](../../internal/api/handlers_node_utilization.go) |
| Threshold settings | [`internal/engine/threshold_settings.go`](../../internal/engine/threshold_settings.go) |

---

## API

| Endpoint | Purpose |
|----------|---------|
| `GET /recommendations/openshift/nodes` | List node recommendations (filters, CSV, `filter[engine]`) |
| `GET /recommendations/openshift/nodes/{node}` | Node detail |
| `GET /recommendations/openshift/machinesets` | Tier 1 MachineSet aggregation (groups node rows) |

Deprecated alias: `GET .../nodes/utilization` → use `/nodes`.

---

## Cold-start signal

`meta.data_days_available` (integer) — the number of distinct days of
`daily_node_digests` data for the queried cluster(s). The UI compares this
value against `min_data_days` from the terms API
(`GET .../settings/terms?recommendation_type=node`) to distinguish cold-start
(insufficient data) from genuine "no recommendations" scenarios.

When the cluster has fewer days of data than the term requires, the frontend
can render an informational state (e.g., "Collecting data — X of Y days
available") instead of showing an empty table.

([Issue #84](https://github.com/pgarciaq/ros-ocp-backend/issues/84))

---

## Related docs

- [Recommendation engines — Node](../architecture/recommendation-engines.md#node-recommendations)
- [MachineSet recommendations (Tier 2)](machineset-recommendations.md)
- [Idle detection](idle-detection.md) — idle/zombie on nodes via shared settings
