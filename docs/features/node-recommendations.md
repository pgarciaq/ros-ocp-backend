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

## Related docs

- [Recommendation engines — Node](../architecture/recommendation-engines.md#node-recommendations)
- [MachineSet recommendations (Tier 2)](machineset-recommendations.md)
- [Idle detection](idle-detection.md) — idle/zombie on nodes via shared settings
