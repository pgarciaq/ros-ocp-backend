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
| Recommendation engine | [`librobne/node/recommend.go`](../../librobne/node/recommend.go) |
| Node savings | [`internal/engine/node/savings.go`](../../internal/engine/node/savings.go) |
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

## Visual Insights: Request vs Usage Gap Chart

The node detail endpoint (`GET /recommendations/openshift/nodes/{node}`) returns
`max_cpu_requests_mc` and `max_mem_requests_kib` in each `daily_digests[]` entry.
These represent the maximum aggregate resource requests across all pods on the node
for each day. The frontend renders this alongside P95 usage as an area chart where
the shaded gap highlights overcommitted resources (requests far exceeding actual usage).

When Visual Insights is on, detail also returns sibling `daily_digests_business_hours`
(same row shape; omitted when empty; never merged into `daily_digests`). The UI
draws Peak hours usage (P50/P95 cores/GiB) with the BH rec as a horizontal line
on that series only. Do not overlay BH recs on all-hours charts. Hide Peak hours
charts when the nest is reason-only. BH enrich and the Peak hours chart share
one `daily_node_digests` read ([#517](https://github.com/pgarciaq/ros-ocp-backend/issues/517));
the handler slices in memory so chart windows stay calendar-based (`start_date` /
`end_date`, default 14 days) and enrich stays `MAX(bucket_date)` × max term length.

Gated behind the `ROS_VISUAL_INSIGHTS_ENABLED` Unleash feature toggle.

([Issue #23](https://github.com/pgarciaq/ros-ocp-backend/issues/23))

---

## Related docs

- [Recommendation engines — Node](../architecture/recommendation-engines.md#node-recommendations)
- [MachineSet recommendations (Tier 2)](machineset-recommendations.md)
- [Idle detection](idle-detection.md) — idle/zombie on nodes via shared settings
