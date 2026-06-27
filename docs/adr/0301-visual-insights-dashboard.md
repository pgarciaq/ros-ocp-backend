# ADR-0301: Visual Insights Dashboard

## Status

Proposed

## Phase

Phase 16+

## Context

Most ROS recommendation detail pages currently display only text tables and
numeric summaries, despite the database containing rich time-series data
(daily digests with percentile bands, hourly samples, growth rates). Users
reviewing recommendations lack visual evidence to validate whether a
recommendation is safe or urgent.

Key observations:

1. **Rich data already exists.** Daily digest tables for containers, VMs, PVCs,
   nodes, namespaces, GPUs, and quotas store 14–90 days of percentile metrics.
   Roughly 80% of the data needed for meaningful visualizations is already
   collected and queryable.

2. **User need is clear.** Operators want to see:
   - When utilization spikes occur (heatmaps, trend lines)
   - Whether growth is linear or exponential (projections)
   - Whether a VM should be powered off overnight (activity patterns)
   - How much headroom exists before hitting quota limits

3. **Competing tools set expectations.** Grafana dashboards, Kubecost, and
   cloud-native cost tools all provide chart-based insight views. Text-only
   recommendations feel underpowered.

4. **PatternFly Charts are already in the stack.** `@patternfly/react-charts`
   (Victory-based) is a dependency in koku-ui, removing any library adoption
   barrier.

## Decision

Add a **Visual Insights** feature layer that provides charts and diagrams across
all recommendation entity types, rolled out in three phased tiers:

- **Tier 1:** Pure frontend charts using data already returned by existing API
  endpoints — zero backend changes, zero new storage.
- **Tier 2:** Charts requiring modest backend additions (exposing hidden fields,
  new hourly aggregation tables for heatmaps).
- **Tier 3:** Strategic enhancements requiring list-page query changes (sparklines,
  fleet-wide dashboards).

### Charting Library

Use **PatternFly Charts** (`@patternfly/react-charts` wrapping Victory). Already a
transitive dependency in koku-ui. Consistent with Red Hat design system guidelines.

### Tier 1 — Low Effort, Data Already Exposed (Phase 1)

All Tier 1 visualizations render client-side from data already present in existing
API responses (detail endpoint `daily_digests` arrays, settings, or list metadata).

| Entity | Visualization | Data Source |
|--------|--------------|-------------|
| Container | OOM event timeline (scatter plot on date axis) | `oom_kill_count` in daily digests |
| Container | CPU throttle trend (area chart, throttled vs total) | `cpu_throttle_*` percentile fields |
| PVC | Storage growth projection (line + dashed extrapolation) | `capacity_bytes` and `usage_bytes_*` over time |
| PVC | Utilization gauge (current usage / capacity) | Latest digest row |
| VM | Resource sizing bar chart (current vs recommended vCPU/GiB) | Recommendation response fields |
| VM | CPU + memory utilization trend (14-day line chart) | `daily_vm_digests` percentile columns |
| Cluster Quota | Hard vs used trend chart (stacked area) | `daily_quota_digests` hard/used columns |
| Cluster Quota | Utilization gauge per resource dimension | Latest digest row |
| Namespace | Quota headroom trend (line: limit − used over time) | `daily_namespace_digests` |
| Snapshot | Age distribution histogram (buckets: <7d, 7–30d, 30–90d, 90d+) | `snapshot_digests.age_days` |
| Snapshot | Cost by type donut chart | Snapshot list aggregation |
| GPU | VRAM utilization gauge | `gpu_container_digests` memory fields |
| Node | Request vs usage gap chart (grouped bar: cpu/mem) | `daily_node_digests` |
| Node | Pod scheduling headroom gauge (used/schedulable pods) | `max_pod_count` / `pod_allocatable` |

**Backend effort:** None.  
**Storage impact:** None (zero new tables or columns).  
**Risk:** Negligible — purely additive UI components.

### Tier 2 — Medium Effort (Phase 2)

| Entity | Visualization | Requirement | Effort |
|--------|--------------|-------------|--------|
| VM | I/O sparkline (IOPS trend) | Expose `disk_read_iops_p95`, `disk_write_iops_p95`, `disk_read_bps_p95`, `disk_write_bps_p95` in VM detail API | ~20 lines backend (fields already in DB, just not serialized) |
| VM | Disk growth projection | Same IOPS fields + capacity trend | Frontend only once fields exposed |
| Node | CPU/memory utilization trend (14–30 days) | Expose node daily digests in node detail API | ~50 lines backend (new detail sub-query) |
| GPU | Utilization radar chart (SM, tensor, DRAM axes) | Multi-axis chart from existing GPU digest columns | Frontend only |
| Container/Namespace | Business hours vs all-hours overlay | Both digest variants already exist; render overlay | Frontend only |
| VM | Activity heatmap (hour-of-day × day-of-week) | **New `hourly_vm_digests` table** | ~200 lines backend + migration |
| Node | Utilization heatmap (hour-of-day × day-of-week) | **New `hourly_node_digests` table** | ~200 lines backend + migration |

### Tier 3 — Strategic, Higher Effort (Phase 3)

| Entity | Visualization | Requirement |
|--------|--------------|-------------|
| All lists | 7-day sparklines (mini-trend per row) | Optional `?include=sparkline` query param; +1 sub-query per list page |
| Node fleet | Fleet heatmap (nodes colored by utilization, grouped by machineset) | Aggregation query joining node digests with machineset labels |
| Cross-entity | Savings waterfall dashboard (breakdown by entity type) | Composite query across all recommendation tables |

---

## Storage and Compute Analysis

Assumptions: 500 VMs, 100 nodes, 90-day retention, ~180 bytes per hourly row.

| Feature | New Tables | Storage Impact | Query Impact | Phase |
|---------|-----------|----------------|--------------|-------|
| Tier 1 charts | None | 0 (existing data) | Negligible (existing queries) | 1 |
| VM hourly digests | `hourly_vm_digests` | ~242 MB (500 × 90 × 24 × 180B) | Sub-10ms per VM heatmap (indexed lookup) | 2 |
| Node hourly digests | `hourly_node_digests` | ~39 MB (100 × 90 × 24 × 180B) | Sub-10ms per node heatmap (indexed lookup) | 2 |
| IOPS in VM API | None (column exists) | 0 | +4 fields per daily digest row | 2 |
| Sparklines in lists | None | 0 | +1 query per list page (7 recent digests × items) | 3 |
| **Total Tier 1+2** | **2 new tables** | **~281 MB** | **<50ms per detail page** | — |

At 5,000 VMs the hourly table grows to ~2.4 GB — still well within PostgreSQL
comfort zone, and bounded by configurable retention.

---

## Configuration / Feature Gating

Resource-intensive features are gated via Viper configuration with environment
variable overrides. Tier 1 charts have zero compute/storage overhead (frontend-only),
so they are always enabled when the master toggle is on.

```yaml
visual_insights:
  enabled: true                          # Master toggle
  hourly_vm_digests:
    enabled: true                        # Enables VM activity heatmap
    retention_days: 90                   # How long to keep hourly data
  hourly_node_digests:
    enabled: true                        # Enables node utilization heatmap
    retention_days: 90
  sparklines_in_lists:
    enabled: false                       # Off by default (adds query load)
    lookback_days: 7
```

Environment variable mapping:

| Variable | Default | Controls |
|----------|---------|----------|
| `ROS_VISUAL_INSIGHTS_ENABLED` | `true` | Master toggle for all visual insights |
| `ROS_HOURLY_VM_DIGESTS_ENABLED` | `true` | VM activity heatmap data collection |
| `ROS_HOURLY_VM_DIGESTS_RETENTION_DAYS` | `90` | Retention for hourly VM rows |
| `ROS_HOURLY_NODE_DIGESTS_ENABLED` | `true` | Node utilization heatmap data collection |
| `ROS_HOURLY_NODE_DIGESTS_RETENTION_DAYS` | `90` | Retention for hourly node rows |
| `ROS_SPARKLINES_ENABLED` | `false` | List-page sparklines (opt-in) |
| `ROS_SPARKLINES_LOOKBACK_DAYS` | `7` | Days of sparkline history to return |

**Rationale:** Tier 1 is pure frontend rendering — no server-side cost. Tier 2
heatmaps require new tables with non-trivial storage, so they get individual
toggles. Sparklines add query overhead to every list request, so they default off.

---

## API Changes

### New Endpoints (Tier 2)

```
GET /api/cost-management/v1/recommendations/openshift/vm/hourly-activity
    ?vm_name=X&cluster_uuid=Y&namespace=Z&days=14
```

Response: array of `{report_date, hour, sample_count, cpu_usage_p95_mc, mem_usage_p95_kib}`
(24 × days entries).

```
GET /api/cost-management/v1/recommendations/openshift/node/hourly-utilization
    ?node_name=X&cluster_uuid=Y&days=14
```

Response: same structure with node-specific fields.

### Existing Endpoint Changes

**VM detail** (`GET .../vm/detail`): Add to each `daily_digests` item:
- `disk_read_iops_p95` (integer, nullable)
- `disk_write_iops_p95` (integer, nullable)
- `disk_read_bps_p95` (integer, nullable)
- `disk_write_bps_p95` (integer, nullable)

**All list endpoints** (Tier 3): When `?include=sparkline` is set, append
`sparkline_data` to each item — an array of 7 recent digest p95 values for the
primary metric (CPU for containers/VMs/nodes, usage_bytes for PVCs).

---

## Database Migrations

### `hourly_vm_digests`

```sql
CREATE TABLE hourly_vm_digests (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    namespace TEXT NOT NULL,
    vm_name TEXT NOT NULL,
    report_date DATE NOT NULL,
    hour SMALLINT NOT NULL CHECK (hour >= 0 AND hour < 24),
    cpu_usage_p95_mc INTEGER,
    mem_usage_p95_kib BIGINT,
    disk_read_iops_p95 INTEGER,
    disk_write_iops_p95 INTEGER,
    sample_count SMALLINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_hourly_vm_digests_lookup
    ON hourly_vm_digests (org_id, cluster_uuid, vm_name, report_date, hour);

CREATE INDEX idx_hourly_vm_digests_retention
    ON hourly_vm_digests (report_date);
```

### `hourly_node_digests`

```sql
CREATE TABLE hourly_node_digests (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    node_name TEXT NOT NULL,
    report_date DATE NOT NULL,
    hour SMALLINT NOT NULL CHECK (hour >= 0 AND hour < 24),
    cpu_usage_p95_mc INTEGER,
    mem_usage_p95_kib BIGINT,
    max_pod_count INTEGER,
    sample_count SMALLINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_hourly_node_digests_lookup
    ON hourly_node_digests (org_id, cluster_uuid, node_name, report_date, hour);

CREATE INDEX idx_hourly_node_digests_retention
    ON hourly_node_digests (report_date);
```

Retention cleanup runs as a periodic task (daily), deleting rows older than
the configured `retention_days`.

---

## Consequences

### Positive

- **Rich visual evidence** for every recommendation type increases user confidence
  in accepting sizing changes.
- **Phased approach** minimizes risk: Tier 1 is zero-risk (frontend only), Tier 2
  is bounded (two tables, configurable), Tier 3 is opt-in.
- **Differentiates from competitors** — few open-source tools provide recommendation
  evidence charts integrated into the detail view.
- **Reuses existing infrastructure** — PatternFly Charts, PostgreSQL, existing daily
  digests. No new external dependencies.

### Negative

- **Hourly tables add ~281 MB** at medium scale (500 VMs, 100 nodes, 90 days).
  Mitigated by configurable retention and per-feature toggles.
- **Sparklines in lists add query overhead** (~7 extra rows per item fetched per
  list request). Mitigated by defaulting off and requiring explicit opt-in.
- **Frontend bundle size** increases slightly from additional chart components.
  Mitigated by code-splitting (lazy-load chart components only on detail pages).

### Neutral

- No changes to the recommendation engine logic itself — visual insights display
  data, they don't affect recommendations.
- No changes to the koku-metrics-operator — all data is already collected at
  sufficient granularity.

---

## Alternatives Considered

### 1. Daily bar charts only (no hourly data)

Rejected. Loses the "when to power off" insight for VMs and the utilization
pattern visibility for nodes. Hour-of-day heatmaps are the primary differentiator
for the VM entity type.

### 2. Client-side aggregation from raw samples

Rejected. Raw 15-minute samples for 500 VMs over 90 days would be ~4.3 million
rows transferred to the browser. Unacceptable latency and bandwidth cost. Server-side
hourly pre-aggregation keeps API responses under 2 KB per heatmap.

### 3. Separate time-series microservice (InfluxDB / TimescaleDB)

Rejected. Over-engineered for this scale. PostgreSQL handles hourly granularity for
≤10k entities without issue. Adding a new database to the deployment stack
contradicts the on-prem simplicity goal.

### 4. Embed Grafana panels via iframe

Rejected. Requires Grafana deployment (not always available on-prem), breaks
PatternFly design consistency, and creates authentication/CORS complexity. Native
charts in the application provide a tighter, more accessible experience.
