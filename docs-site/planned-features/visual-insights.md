# Visual Insights (Planned)

!!! warning "Status: Planned / Future Work"
    Visual Insights is a **planned feature** that will add charts, gauges, and
    heatmaps to recommendation detail pages across all entity types. Tier 1
    (frontend-only charts) is targeted for the next release; Tiers 2 and 3 follow
    in subsequent quarters. This page may change before implementation.

!!! info "Quick Facts"
    **Scope:** Charts and diagrams for all ROS recommendation entity types  
    **Backend changes:** Tier 1 requires none; Tier 2 adds two hourly tables (~281 MB at medium scale)  
    **Charting library:** PatternFly Charts (`@patternfly/react-charts` / Victory)  
    **Feature-gated:** Yes — resource-intensive features (heatmaps, sparklines) are individually toggleable  
    **ADR:** [0301-visual-insights-dashboard](../../docs/adr/0301-visual-insights-dashboard.md)

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
| **2 (Tier 2)** | Medium | Heatmaps, I/O trends, overlay charts | 2 new tables, 4 exposed fields |
| **3 (Tier 3)** | Higher | Sparklines in lists, fleet dashboards | Optional list-query enhancement |

---

## Visualizations by Entity

### Virtual Machines

**Phase 1:**

- **Resource sizing bar chart** — Side-by-side comparison of current vCPU/GiB
  allocation vs the recommended values, making over-provisioning immediately visible.
- **CPU + memory utilization trend** — 14-day line chart showing daily p95
  utilization, with the recommendation threshold overlaid.

![VM Resource Sizing Chart](../assets/visual-insights-vm-sizing-chart.png)

**Phase 2:**

- **I/O sparkline** — Disk read/write IOPS trend (requires exposing four existing
  database columns in the API response).
- **Disk growth projection** — Extrapolated line showing when current capacity
  will be exhausted at the observed growth rate.
- **Activity heatmap** — Hour-of-day × day-of-week grid colored by CPU utilization,
  revealing idle periods (e.g., "this VM is unused 8 PM–6 AM and all weekends").

![VM Activity Heatmap](../assets/visual-insights-vm-heatmap.png)

---

### Nodes

**Phase 1:**

- **Request vs usage gap chart** — Grouped bar chart showing CPU and memory
  requests alongside actual usage, highlighting wasted reservations.
- **Pod scheduling headroom gauge** — Visual gauge showing how close the node is
  to its pod scheduling limit.

**Phase 2:**

- **CPU/memory utilization trend (14–30 days)** — Line chart showing node-level
  utilization over time with safe-to-consolidate threshold overlaid.
- **Utilization heatmap** — Same hour-of-day × day-of-week format as VMs, useful
  for identifying nodes that are idle during off-hours.

![Node Utilization Heatmap](../assets/visual-insights-node-heatmap.png)

---

### Containers

**Phase 1:**

- **OOM event timeline** — Scatter plot showing out-of-memory kill events on a
  date axis, making it easy to spot recurring patterns.
- **CPU throttle trend** — Area chart showing throttled CPU time as a percentage,
  overlaid with total CPU usage.

**Phase 2:**

- **Business hours vs all-hours overlay** — Dual-line chart comparing utilization
  during business hours (as configured) vs the full 24-hour window.

---

### Persistent Volume Claims (PVCs)

**Phase 1:**

- **Storage growth projection** — Line chart of historical usage with a dashed
  extrapolation line showing projected exhaustion date.
- **Utilization gauge** — Current usage as a percentage of provisioned capacity,
  with color thresholds (green/amber/red).

![PVC Growth Projection](../assets/visual-insights-pvc-projection.png)

---

### Namespaces

**Phase 1:**

- **Quota headroom trend** — Line chart showing the gap between quota limit and
  actual usage over time, highlighting namespaces approaching their ceiling.

**Phase 2:**

- **Business hours vs all-hours overlay** — Same dual-line format as containers,
  applied at the namespace aggregation level.

---

### GPUs

**Phase 1:**

- **VRAM utilization gauge** — Visual gauge showing GPU memory usage relative to
  device capacity.

**Phase 2:**

- **Utilization radar chart** — Multi-axis chart showing SM utilization, tensor
  core activity, and DRAM bandwidth simultaneously, helping identify which GPU
  subsystem is the bottleneck.

---

### Cluster Resource Quotas

**Phase 1:**

- **Hard vs used trend chart** — Stacked area chart showing how quota consumption
  evolves relative to the hard limit.
- **Utilization gauge per resource** — One gauge each for CPU, memory, and pods
  showing current vs hard limit.

---

### Snapshots

**Phase 1:**

- **Age distribution histogram** — Bar chart with buckets (<7 days, 7–30 days,
  30–90 days, 90+ days) showing how many snapshots fall into each age category.
- **Cost by type donut chart** — Proportional view of snapshot storage cost by
  snapshot type.

---

## Phase 3: List-Page Enhancements

**Sparklines in all list views:**

Every recommendation list page (containers, VMs, nodes, PVCs, namespaces, GPUs)
will optionally display a 7-day mini-trend sparkline for the primary metric. This
is requested via `?include=sparkline` and defaults to **off** because it adds one
additional query per list page load.

**Node fleet heatmap:**

A dashboard view showing all nodes colored by utilization and grouped by
MachineSet, giving platform teams a single-glance view of fleet health.

**Savings waterfall dashboard:**

Cross-entity breakdown showing total potential savings by category (VMs, nodes,
containers, PVCs) in a waterfall chart format.

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
| Phase 1 (Tier 1) | Next release | Planned |
| Phase 2 (Tier 2) | Following quarter | Planned |
| Phase 3 (Tier 3) | Future | Under consideration |

---

## Related

- [ADR-0301: Visual Insights Dashboard](../../docs/adr/0301-visual-insights-dashboard.md)
- [Virtual Machine Recommendations](../features/virtual-machines.md)
- [Node Recommendations](../features/node-recommendations.md)
- [Container Recommendations](../features/container-recommendations.md)
- [PVC Rightsizing](../features/pvc-rightsizing.md)
- [Cluster Resource Quota](../features/cluster-resource-quota.md)
