# Visual Insights (Shipped)

> **Last verified:** 2026-09-14

!!! success "Status: Complete — All Phases Shipped"
    Visual Insights adds charts, gauges, and heatmaps to recommendation detail
    pages across all entity types. **Phase 1 (Tier 1), Phase 2 (Tier 2), and
    most of Phase 3 (Tier 3) are now fully implemented.** All visualizations
    listed below are shipped unless explicitly marked otherwise. The only
    remaining planned item is sparklines in list views.

!!! info "Quick Facts"
    **Scope:** Charts and diagrams for all ROS recommendation entity types  
    **Backend changes:** Tier 1 requires minimal changes (OOM timeline endpoint + throttle field in boxplots); Tier 2 adds two hourly tables (~281 MB at medium scale)  
    **Charting library:** PatternFly Charts (`@patternfly/react-charts` / Victory)  
    **Feature-gated:** Yes — resource-intensive features (heatmaps, sparklines) are individually toggleable  
    **ADR:** [0301-visual-insights-dashboard](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0301-visual-insights-dashboard.md)

---

## Why Visual Insights?

Today, ROS recommendation pages show numeric tables — current values, recommended
values, and percentage deltas. While accurate, this format makes it hard to:

- **Spot patterns** — Is the VM idle every night? Does CPU spike on Mondays?
- **Build confidence** — Is the recommendation based on a stable trend or a one-off spike?
- **Project forward** — Will this PVC run out of space in 30 days or 6 months?

Visual Insights adds **charts and diagrams** that answer these questions at a glance,
using data the system already collects.

---

## Phase Overview

| Phase | Effort | What Ships | Backend Changes |
|-------|--------|-----------|-----------------|
| **1 (Tier 1)** | Low | Charts using existing API data | None |
| **2 (Tier 2)** | Medium | Heatmaps, overlay charts | 2 new tables (~200 lines Go + 1 migration each) |
| **3 (Tier 3)** | Higher | Sparklines in lists, fleet dashboards | Optional list-query enhancement |

---

## Visualizations by Entity

### Virtual Machines

**Phase 1:**

- **Resource sizing bar chart** — Side-by-side comparison of current vCPU/GiB
  allocation vs the recommended values, making over-provisioning immediately visible.
  **Implemented** — see [Issue #7](https://github.com/pgarciaq/ros-ocp-backend/issues/7).
- **CPU + memory utilization trend** — 14-day line chart showing daily p95
  utilization, with the recommendation threshold overlaid.
  **Implemented** — see [Issue #8](https://github.com/pgarciaq/ros-ocp-backend/issues/8).
- **I/O sparkline** — Compact dual sparklines (IOPS + throughput) showing daily
  disk read/write trends. The daily I/O fields (`disk_read_iops_p95`,
  `disk_write_iops_p95`, `disk_read_bps_p95`, `disk_write_bps_p95`) are now
  exposed in the `daily_digests` response, gated by `ROS_VISUAL_INSIGHTS_ENABLED`.
  **Implemented** — see [Issue #9](https://github.com/pgarciaq/ros-ocp-backend/issues/9).
- **Disk growth projection** — Extrapolated line showing when current capacity
  will be exhausted at the observed growth rate, using existing IOPS and capacity fields.

![VM Resource Sizing Chart](../assets/visual-insights-vm-sizing-chart.png)

**Phase 2:**

- **Activity heatmap** — Hour-of-day × day-of-week grid colored by CPU utilization,
  revealing idle periods (e.g., "this VM is unused 8 PM–6 AM and all weekends").
  Rendered using `VictoryScatter` with square-sized markers. Displays a "Data
  available from [deploy date]" note since historical hourly data cannot be backfilled.
  **Backend endpoint implemented** — `GET /vm/hourly-activity` serves hourly
  CPU, memory, and disk I/O digests from `hourly_vm_digests`. Gated by
  `ROS_VISUAL_INSIGHTS_ENABLED` and `ROS_HOURLY_VM_DIGESTS_ENABLED`.
  **Implemented** — see [Issue #13](https://github.com/pgarciaq/ros-ocp-backend/issues/13).

![VM Activity Heatmap](../assets/visual-insights-vm-heatmap.png)

---

### Nodes

**Phase 1:**

- **Request vs usage gap chart** — Grouped bar chart showing CPU and memory
  requests alongside actual usage, highlighting wasted reservations.
- **Pod scheduling headroom gauge** — Visual gauge showing how close the node is
  to its pod scheduling limit.
  **Implemented** — see [Issue #26](https://github.com/pgarciaq/ros-ocp-backend/issues/26) and [Issue #100](https://github.com/pgarciaq/ros-ocp-backend/issues/100).

**Phase 2:**

- **CPU/memory utilization trend (14–30 days)** — Line chart showing node-level
  utilization over time with safe-to-consolidate threshold overlaid.
  **Implemented** — see [Issue #20](https://github.com/pgarciaq/ros-ocp-backend/issues/20).
- **Utilization heatmap** — Same hour-of-day × day-of-week format as VMs, useful
  for identifying nodes that are idle during off-hours. Displays a "Data available
  from [deploy date]" note since historical hourly data cannot be backfilled.
  **Backend endpoint implemented** — `GET /node/{id}/hourly-utilization` serves
  hourly CPU and memory digests from `hourly_node_digests`. Gated by
  `ROS_VISUAL_INSIGHTS_ENABLED` and `ROS_HOURLY_NODE_DIGESTS_ENABLED`.
  **Implemented** — see [Issue #16](https://github.com/pgarciaq/ros-ocp-backend/issues/16).

![Node Utilization Heatmap](../assets/visual-insights-node-heatmap.png)

---

### Containers

**Phase 1:**

- **OOM event timeline** — Scatter plot showing out-of-memory kill events on a
  date axis, making it easy to spot recurring patterns. Served by a dedicated
  endpoint ([ADR-0302](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0302-oom-timeline-endpoint.md)):

    ```
    GET /api/cost-management/v1/recommendations/openshift/containers/{id}/oom-timeline
        ?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD
    ```

    Returns sparse data (only days with OOM events). The frontend fetches this
    lazily when the user expands the OOM section. See the
    [OOM Timeline API reference](../api-reference/oom-timeline.md) for full details.
    **Implemented** — see [Issue #3](https://github.com/pgarciaq/ros-ocp-backend/issues/3).

- **CPU throttle trend** — Area chart showing throttled CPU time (p95 + max),
  overlaid with total CPU usage. Data is served via the `cpuThrottle` field in
  the existing boxplot response (`plots_data`), scoped to the recommendation term
  window. Values are in cores (converted from millicores). No new endpoint needed.

    ```json
    "cpuThrottle": { "p95": 0.042, "max": 0.185, "format": "cores" }
    ```

    **Implemented** — see [Issue #4](https://github.com/pgarciaq/ros-ocp-backend/issues/4).

**Phase 2:**

- **Business hours vs all-hours overlay** — Dual-line chart comparing utilization
  during business hours (as configured) vs the full 24-hour window.
  **Implemented** — see [Issue #18](https://github.com/pgarciaq/ros-ocp-backend/issues/18).

---

### Persistent Volume Claims (PVCs)

**Phase 1:**

- **Storage growth projection** — Line chart of historical usage with a dashed
  extrapolation line showing projected exhaustion date.
  **Implemented** — see [Issue #5](https://github.com/pgarciaq/ros-ocp-backend/issues/5).
- **Utilization gauge** — Current usage as a percentage of provisioned capacity,
  with color thresholds (green/amber/red).
  **Implemented** — see [Issue #6](https://github.com/pgarciaq/ros-ocp-backend/issues/6).

![PVC Growth Projection](../assets/visual-insights-pvc-projection.png)

---

### Namespaces

**Phase 1:**

- **Quota headroom trend** — Line chart showing the gap between quota hard limit and
  actual usage over time for CPU request and memory request, highlighting namespaces
  approaching their ceiling. Served by a dedicated endpoint:

    ```
    GET /api/cost-management/v1/recommendations/openshift/quota/{quota-id}/trend
        ?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD
    ```

    Returns daily `cpu_request_hard_millicores`, `cpu_request_used_millicores`,
    `memory_request_hard_bytes`, and `memory_request_used_bytes`. Defaults to last
    30 days. The gap between hard and used represents headroom.
    **Implemented** — see [Issue #14](https://github.com/pgarciaq/ros-ocp-backend/issues/14).

**Phase 2:**

- **Business hours vs all-hours overlay** — Same dual-line format as containers,
  applied at the namespace aggregation level.
  **Implemented** — see [Issue #18](https://github.com/pgarciaq/ros-ocp-backend/issues/18).

---

### GPUs

**Phase 1:**

- **VRAM utilization gauge** — Visual gauge showing GPU memory usage relative to
  device capacity.
  **Implemented** — see [Issue #21](https://github.com/pgarciaq/ros-ocp-backend/issues/21).

**Phase 2:**

- **Utilization radar chart** — Multi-axis chart showing SM utilization, tensor
  core activity, and DRAM bandwidth simultaneously, helping identify which GPU
  subsystem is the bottleneck.
  **Implemented** — see [Issue #17](https://github.com/pgarciaq/ros-ocp-backend/issues/17).

---

### Cluster Resource Quotas

**Phase 1:**

- **Hard vs used trend chart** — Stacked area chart showing how quota consumption
  evolves relative to the hard limit.
  **Implemented** — see [Issue #11](https://github.com/pgarciaq/ros-ocp-backend/issues/11).
- **Utilization gauge per resource** — One gauge each for CPU, memory, and pods
  showing current vs hard limit.
  **Implemented** — see [Issue #12](https://github.com/pgarciaq/ros-ocp-backend/issues/12).

---

### Snapshots

**Phase 1:**

- **Age distribution histogram** — Bar chart with buckets (<7 days, 7–30 days,
  30–90 days, 90+ days) showing how many snapshots fall into each age category.
  **Backend endpoint implemented** — `GET /snapshots/age-distribution` returns
  bucketed snapshot counts. Gated by `ROS_VISUAL_INSIGHTS_ENABLED`.
  **Implemented** — see [Issue #15](https://github.com/pgarciaq/ros-ocp-backend/issues/15).
- **Cost by type donut chart** — Proportional view of snapshot storage cost by
  snapshot type.
  **Backend endpoint implemented** — `GET /snapshots/cost-by-type` returns
  costs grouped by recommendation type. Gated by `ROS_VISUAL_INSIGHTS_ENABLED`.
  **Implemented** — see [Issue #19](https://github.com/pgarciaq/ros-ocp-backend/issues/19).

---

## Phase 3: List-Page Enhancements

**Sparklines in all list views:**

Every recommendation list page (containers, VMs, nodes, PVCs, namespaces, GPUs)
will optionally display a 7-day mini-trend sparkline for the primary metric. This
is requested via `?include=sparkline` and defaults to **off** because it adds one
additional query per list page load.

**Node fleet heatmap:** ✅ Complete

A dashboard view showing all nodes colored by utilization and grouped by
MachineSet, giving platform teams a single-glance view of fleet health.
**Implemented** — see [Issue #24](https://github.com/pgarciaq/ros-ocp-backend/issues/24).

### Fleet summary API

```
GET /api/cost-management/v1/recommendations/openshift/fleet-summary
```

Organization-wide container health rollup. Counts only **medium-term,
cost-engine** rows in `recommendation_sets`; `active_containers` and
`idle_containers` are mutually exclusive (idle = non-stale rows with
notification code **5**; active = non-stale rows without code 5), so
`active + idle` ≤ non-stale count. `abandoned_containers` counts rows with
`idle_state = 'zombie'`. No query parameters (OpenAPI `parameters: []`).

**Response shape** (`FleetSummaryResponse` in `openapi.json`, handler
`GetFleetSummary` in `internal/api/handlers_fleet.go`):
`total_containers`, `active_containers`, `idle_containers`,
`abandoned_containers`, `total_monthly_savings` (`MoneyAmount` with
`value` + `units`), `cluster_count`, `currency`.

Real response recorded 2026-09-14 against local API (org `3340851`,
one cluster, trimmed — full payload shown, only 7 fields):

```bash
IDENTITY=$(echo -n '{"identity":{"account_number":"10001","org_id":"3340851","type":"User","user":{"username":"admin","email":"admin@example.com","is_org_admin":true}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)
curl -s -H "x-rh-identity: $IDENTITY" \
  http://localhost:8000/api/cost-management/v1/recommendations/openshift/fleet-summary
```

```json
{
  "total_containers": 31,
  "active_containers": 31,
  "idle_containers": 0,
  "abandoned_containers": 0,
  "total_monthly_savings": { "value": "0.00", "units": "USD" },
  "cluster_count": 1,
  "currency": "USD"
}
```

**Errors (honestly triggerable):**

| Status | When | Evidence |
|--------|------|----------|
| `401` | Missing or unparseable `x-rh-identity` | Verified live: no header → `{"message":"Unable to unmarshal X-Rh-Identity into struct"}` |
| `503` | DB pool unavailable or summary query fails (`database connection unavailable` / `unable to fetch fleet summary`) | From handler code paths; not triggered against the healthy local DB |
| `200` with zeroed counts | RBAC cluster filter matches nothing (scoped caller, no allowed clusters) | From handler code path; not triggered live (local caller is org admin) |

**Cache:** in-memory LRU+TTL (`internal/fleetsummary/cache.go`, ADR-0112
pattern). Key is org + RBAC scope (`CacheKey`: `orgID:all` or
`orgID:rbac:<hash>`). TTL `ROS_FLEET_SUMMARY_CACHE_TTL` (default **300 s**),
capacity `ROS_FLEET_SUMMARY_CACHE_CAPACITY` (default **256** entries).
Invalidation (`InvalidateOrg`, prefix-drop per org) fires on recommendation
ingest (`internal/engine/recommend_all.go`), threshold recalculation
(`threshold_recalculate.go`, `threshold_recalc_guard.go`), business-hours
settings changes (`handlers_business_hours_settings.go`), savings
recalculation (`savings_recalculate.go`, `savings_recalc_guard.go`),
retention sweeps (`retention.go`), reship triggers
(`internal/reship/trigger_guard.go`), and source cleanup
(`housekeeper/sourcesCleaner.go`). Observability:
`rosocp_fleet_summary_cache_{size,hits_total,misses_total,removals_total,invalidations_total}`.
See [Monitoring](../monitoring.md#fleet-summary-cache).

### Node fleet heatmap API

```
GET /api/cost-management/v1/recommendations/openshift/fleet-heatmap
```

Per-node utilization cells for the fleet heatmap. Nodes are returned
**ungrouped** — the client groups by `machineset_name`. Each row carries a
server-computed `utilization_band` (`idle` / `low` / `moderate` / `healthy` /
`hot`) derived from the selected metric's p95 plus `idle_state`
(`UtilizationBand` in `internal/api/handlers_fleet_heatmap.go`: `idle` when
idle/zombie or p95 < 0.10; `low` < 0.30; `moderate` < 0.65; `healthy` < 0.85;
else `hot`).

**Gating:** registered only when the native recommendation routes are active
**and** `ROS_VISUAL_INSIGHTS_ENABLED=true`
(`internal/api/server.go`: `if nativeRecommendationRoutes &&
config.VisualInsightsEnabled()`). Default is **on** here
(`ROS_VISUAL_INSIGHTS_ENABLED` defaults to `true`). When the toggle is off
the route is not registered (OpenAPI documents this as `404 Visual insights
feature is not enabled`).

**Parameters:**

| Param | Style | Values | Default |
|-------|-------|--------|---------|
| `metric` | flat `?metric=` | `cpu`, `memory` — selects which p95 drives `utilization_band` | `cpu` |
| `filter[term]` | bracket | `short`, `medium`, `long` | `medium` |
| `filter[engine]` | bracket | `cost`, `performance` | `cost` |
| `filter[cluster]` | bracket | cluster UUID (narrows scope) | all org clusters |

Data-window labels echo the term: short → `1 day (short term p95)`, medium →
`7 days (medium term p95)`, long → `15 days (long term p95)`.

**Response shape** (`FleetHeatmapResponse` in `openapi.json`): `meta`
(`count`, `metric`, `term`, `engine`, `latest_update` (RFC 3339),
`data_window`, `currency`, optional `warnings`) plus `data[]` per node:
`node`, `cluster_uuid`, `cluster_alias` (falls back to UUID when unset),
`machineset_name` (empty when none), `instance_type`, `cpu_util_p95`,
`mem_util_p95`, `idle_state`, `utilization_band`, `node_count_reduction`,
`estimated_savings_cents`.

Real response recorded 2026-09-14 against local API (org `3340851`,
`metric=cpu`, `term=medium`, `engine=cost`; `meta` full, `data` trimmed to
2 of 7 nodes):

```bash
curl -s -H "x-rh-identity: $IDENTITY" \
  "http://localhost:8000/api/cost-management/v1/recommendations/openshift/fleet-heatmap"
```

```json
{
  "meta": {
    "count": 7,
    "metric": "cpu",
    "term": "medium",
    "engine": "cost",
    "latest_update": "2026-09-14T03:05:27+02:00",
    "data_window": "7 days (medium term p95)",
    "currency": "USD"
  },
  "data": [
    {
      "node": "gpu-mig-1",
      "cluster_uuid": "550e8400-e29b-41d4-a716-446655440001",
      "cluster_alias": "my-cluster",
      "machineset_name": "gpu-mig",
      "instance_type": "",
      "cpu_util_p95": 17.3377,
      "mem_util_p95": 6.7902,
      "idle_state": "active",
      "utilization_band": "hot",
      "node_count_reduction": 0,
      "estimated_savings_cents": 0
    },
    {
      "node": "gpu-t4-1",
      "cluster_uuid": "550e8400-e29b-41d4-a716-446655440001",
      "cluster_alias": "my-cluster",
      "machineset_name": "gpu-t4",
      "instance_type": "",
      "cpu_util_p95": 6.6389,
      "mem_util_p95": 0.726,
      "idle_state": "active",
      "utilization_band": "hot",
      "node_count_reduction": 0,
      "estimated_savings_cents": 0
    }
  ]
}
```

Verified live: `?metric=memory` returns `meta.metric: "memory"`;
`filter[cluster]=550e8400-e29b-41d4-a716-446655440001` returns the same 7
nodes; `filter[term]=short` + `filter[engine]=performance` returns
`term: "short"`, `engine: "performance"`, `data_window: "1 day (short term
p95)"`. Unknown cluster / RBAC-narrowed callers get HTTP 200 with
`count: 0` and `data: []` (handler code path; integration test
`TestGetFleetHeatmap_EmptyState` covers the empty-DB shape).

**Errors (honestly triggerable, all verified live):**

| Status | When | Evidence |
|--------|------|----------|
| `400` | `?metric=disk` | `{"status":"error","message":"invalid metric; must be 'cpu' or 'memory'"}` |
| `400` | `filter[term]=bogus` | `{"status":"error","message":"invalid term; must be 'short', 'medium', or 'long'"}` |
| `400` | `filter[engine]=invalid` | `{"status":"error","message":"invalid engine; must be 'cost' or 'performance'"}` |
| `401` | Missing or unparseable `x-rh-identity` | `{"message":"Unable to unmarshal X-Rh-Identity into struct"}` |
| `404` | `ROS_VISUAL_INSIGHTS_ENABLED=false` (route not registered) | From `server.go` gating + OpenAPI `404`; not triggered live (toggle is on locally) |
| `503` | DB pool unavailable or heatmap query fails (`unable to fetch fleet heatmap data`, incl. heavy-statement timeout) | From handler code paths; not triggered against the healthy local DB |

Results are capped at `ROS_FLEET_HEATMAP_MAX_NODES` (default **1000**);
overflow truncates and appends a `meta.warnings` entry (`Results capped at N
nodes. Filter by cluster to narrow scope.`). Unreadable rows are skipped,
counted, and surfaced as `N row(s) could not be read` warnings
(`rosocp_fleet_heatmap_scan_errors_total`).

**Cache:** dedicated LRU+TTL (`internal/fleetheatmap/cache.go`) sharing the
fleet TTL (`ROS_FLEET_SUMMARY_CACHE_TTL`, default **300 s**) with capacity
`ROS_FLEET_HEATMAP_CACHE_CAPACITY` (default **128**; entries are large —
~200 bytes × `ROS_FLEET_HEATMAP_MAX_NODES`, so 128 × 1000 nodes ≈ 25 MB).
Key extends the fleet RBAC-aware base with
`metric:term:engine[:cluster=]` (`CacheKey`). Same `InvalidateOrg` triggers
as fleet-summary (recommend ingest, threshold/BH settings, savings recalc,
retention, reship, source cleanup). Observability:
`rosocp_fleet_heatmap_cache_{hits_total,misses_total,size,removals_total,invalidations_total}`.
See [Monitoring](../monitoring.md#fleet-summary-cache).

**Savings waterfall dashboard:** ✅ Complete

Cross-entity breakdown showing total potential savings by category (VMs, nodes,
containers, PVCs, GPUs, snapshots) as a horizontal bar chart sorted by magnitude.
Positive savings shown in blue, negative in red. Uses the existing
`/savings-summary` endpoint (`by_plugin` field). Gated by
`ROS_VISUAL_INSIGHTS_ENABLED`.
**Implemented** — see [Issue #25](https://github.com/pgarciaq/ros-ocp-backend/issues/25).

---

## Configuration

Resource-intensive visualizations can be individually toggled by operators.
Tier 1 charts have zero backend overhead and are always available when the
master toggle is enabled.

| Setting | Default | Purpose |
|---------|---------|---------|
| `ROS_VISUAL_INSIGHTS_ENABLED` | `true` | Master toggle for all visual insights |
| `ROS_HOURLY_VM_DIGESTS_ENABLED` | `true` | Enable VM activity heatmap data collection |
| `ROS_HOURLY_VM_DIGESTS_RETENTION_DAYS` | `90` | Days to retain hourly VM data |
| `ROS_HOURLY_NODE_DIGESTS_ENABLED` | `true` | Enable node utilization heatmap data collection |
| `ROS_HOURLY_NODE_DIGESTS_RETENTION_DAYS` | `90` | Days to retain hourly node data |
| `ROS_SPARKLINES_ENABLED` | `false` | Enable sparklines in list views (adds query load) |
| `ROS_SPARKLINES_LOOKBACK_DAYS` | `7` | Number of days of sparkline history to return |

### Storage Impact

At medium scale (500 VMs, 100 nodes, 90-day retention):

| Component | Storage |
|-----------|---------|
| Tier 1 charts | 0 (uses existing data) |
| Hourly VM digests | ~242 MB |
| Hourly node digests | ~39 MB |
| **Total** | **~281 MB** |

Storage scales linearly with entity count and retention period. Operators can
reduce `retention_days` to control disk usage.

---

## Timeline

| Phase | Target | Status |
|-------|--------|--------|
| Phase 1 (Tier 1) | Next release | ✅ Complete — All Tier 1 visualizations shipped (Issues #3, #4, #5, #6, #7, #8, #9, #11, #12, #14, #15, #19, #21, #26) |
| Phase 2 (Tier 2) | Following quarter | ✅ Complete — All Tier 2 visualizations shipped (Issues #13, #16, #17, #18, #20, #100) |
| Phase 3 (Tier 3) | Future | Mostly complete — Node fleet heatmap (#24) and savings waterfall (#25) shipped; sparklines in list views remains planned |

---

## UX Notes

- **Chart placement:** All Visual Insights charts appear **after the
  Configuration/sizing section** on the breakdown page, in a dedicated "Visual
  Insights" card section.
- **Loading strategy:** Detail pages use **eager loading** — chart data is fetched
  with the initial page load to eliminate perceived latency.
- **Data availability indicator:** Heatmaps display a note "Data available from
  [deploy date]" since historical hourly data cannot be backfilled. The date is
  inferred from the earliest row in the hourly digest table for that entity.
- **Tier 1 charts require minimal backend changes** — most data was already exposed
  through existing API endpoints. Two additions were needed: a dedicated OOM timeline
  endpoint ([ADR-0302](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0302-oom-timeline-endpoint.md)) and a
  `cpuThrottle` field in the boxplot response. No new tables or migrations.

---

## Accessibility

All Visual Insights charts include **screen reader support** via visually-hidden
HTML data tables (`.pf-v6-u-screen-reader`) rendered adjacent to every SVG chart.
Charts support full **keyboard navigation** (arrow keys between data elements,
`Escape` to return to chart container, visible focus rings). Heatmaps use a
single-hue blue intensity ramp with numeric values in each cell — color alone
never conveys meaning. The feature targets **WCAG 2.1 AA** compliance per Red Hat
product requirements.

---

## Remaining / follow-up

- **Sparklines in list views** — still gated off by default (`ROS_SPARKLINES_ENABLED=false`); not required for the shipped Visual Insights detail experience.

---

## Related

- [ADR-0301: Visual Insights Dashboard](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0301-visual-insights-dashboard.md)
- [Usage Percentile-Band Plots](percentile-band-plots.md)
- [Virtual Machine Recommendations](virtual-machines.md)
- [Node Recommendations](node-recommendations.md)
- [Container Recommendations](container-recommendations.md)
- [PVC Rightsizing](pvc-rightsizing.md)
- [Cluster Resource Quota](cluster-resource-quota.md)
