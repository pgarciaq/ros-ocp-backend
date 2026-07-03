# API Specification

The ROS-OCP Backend API is documented using the OpenAPI 3.0 specification.

## Viewing the Specification

The authoritative spec is [`openapi.json`](../openapi.json) at the repository root.

You can view it interactively using:

- **Swagger UI**: Paste the raw URL into [editor.swagger.io](https://editor.swagger.io)
- **Redoc**: Use [redocly.github.io/redoc](https://redocly.github.io/redoc/?url=https://raw.githubusercontent.com/pgarciaq/ros-ocp-backend/{{ git_branch }}/openapi.json)
- **Local**: `npx @redocly/cli preview-docs openapi.json`

## Key Endpoints

| Group | Path | Method | Description |
|-------|------|--------|-------------|
| Containers | `/recommendations/openshift` | GET | Container recommendations |
| History & quality | `/recommendations/openshift/history` | GET | Container recommendation history — filters: `filter[engine]`, `filter[term]`, `filter[cluster]`, `filter[namespace]`, `filter[workload]`, `filter[container]`, `filter[tag:<key>]` |
| History & quality | `/recommendations/openshift/quality` | GET | Container recommendation quality metrics — filters: `filter[engine]`, `filter[cluster]`, `filter[namespace]`, `filter[workload]`, `filter[container]`. Per-plugin variants also exist: `/quality/containers`, `/quality/pvcs`, `/quality/vms`, `/quality/gpu`, `/quality/snapshots`. |
| History & quality | `/recommendations/openshift/namespaces/{id}/history` | GET | Namespace recommendation history — filters: `filter[term]`, `filter[engine]` |
| History & quality | `/recommendations/openshift/vms/{vm_name}/history` | GET | VM recommendation history — requires `cluster_uuid` (or `cluster_id`), `namespace`; optional `term`, `engine` |
| GPU | `/recommendations/openshift/gpu` | GET | GPU utilization summary |
| GPU | `/recommendations/openshift/gpu/timeslicing` | GET | Time-slicing recommendations |
| GPU | `/recommendations/openshift/gpu/mig` | GET | MIG partition recommendations |
| Nodes | `/recommendations/openshift/nodes` | GET | Node utilization recommendations |
| MachineSets | `/recommendations/openshift/machinesets` | GET | MachineSet fleet aggregation (Tier 1 — groups node recommendations) |
| PVCs | `/recommendations/openshift/pvcs` | GET | PVC right-sizing list — filters: `filter[cluster]`, `filter[project]`, `filter[storageclass]`, `filter[term]`, `filter[recommendation_type]`, `filter[tag:<key>]`; list rows include `estimated_monthly_savings`, `mounted_by`, `vm_name`, growth (`days_to_full`, `growth_bytes_per_day` on near-full/oversized when projected), orphan idle (`idle_since`, `idle_duration_days` on orphaned); `format=csv` or `Accept: text/csv` for export |
| PVCs | `/recommendations/openshift/pvcs/detail` | GET | PVC detail — requires `cluster_uuid`, `namespace`, `persistentvolumeclaim`; returns all terms (with per-term growth fields when present), `mounted_by`, `vm_name`, plus `historical_usage` daily digests |
| PVCs | `/recommendations/openshift/settings/pvc` | GET/PUT/DELETE | PVC utilization thresholds (`oversized_threshold`, `near_full_threshold`, `min_trend_days`, `days_to_full_alert`, `locked_fields`) |
| PVCs | `/recommendations/openshift/notification-codes` | GET | Notification code catalog — `filter[plugin]=container|namespace|node|gpu|pvc|snapshot|vm|quota|cluster-quota` |
| Fleet | `/recommendations/openshift/fleet-summary` | GET | Fleet-wide recommendation counts and health summary |
| Fleet | `/recommendations/openshift/fleet-heatmap` | GET | Cluster × metric heatmap for fleet overview |
| Fleet | `/recommendations/openshift/savings-summary` | GET | Fleet savings rollup — `by_plugin.pvc` honors `term` only (engine-agnostic); `by_plugin.snapshot` is term-independent (sums all snapshot rows); container, node, and VM honor `engine` (default `cost`) and `term` (default `medium`). **Note:** `term` is a flat query param (`?term=short`) while `engine` uses bracket notation (`?filter[engine]=performance`). |
| Quota | `/recommendations/openshift/quota` | GET | Namespace ResourceQuota right-sizing (`quota` plugin) |
| Quota | `/recommendations/openshift/quota/detail` | GET | ResourceQuota detail |
| Quota | `/recommendations/openshift/cluster-quota` | GET | ClusterResourceQuota right-sizing (`cluster-quota` plugin) |
| Quota | `/recommendations/openshift/cluster-quota/detail` | GET | ClusterResourceQuota detail |
| Namespaces | `/recommendations/openshift/namespaces` | GET | Namespace quota recommendations |
| Snapshots | `/recommendations/openshift/snapshots` | GET | Stale snapshot list |
| Snapshots | `/recommendations/openshift/snapshots/summary` | GET | Snapshot summary statistics (counts by state) |
| Snapshots | `/recommendations/openshift/snapshots/age-distribution` | GET | Snapshot age distribution histogram |
| Snapshots | `/recommendations/openshift/snapshots/cost-by-type` | GET | Snapshot storage cost breakdown by type |
| Settings | `/recommendations/openshift/settings/terms` | GET/PUT/DELETE | Term configuration (`?recommendation_type=<plugin>`) |
| Settings | `/recommendations/openshift/settings/capabilities` | GET | Plugin capabilities |
| Settings | `/recommendations/openshift/settings/snapshot` | GET/PUT/DELETE | Snapshot staleness thresholds; DELETE resets tenant overrides |
| Settings | `/recommendations/openshift/settings/{container,namespace,node,gpu,pvc}` | GET/PUT/DELETE | Per-plugin sizing thresholds (canonical) |
| Settings | `/recommendations/openshift/settings/thresholds` | GET/PUT/DELETE | Deprecated alias (`?recommendation_type=<plugin>`) |
| Settings | `/recommendations/openshift/settings/vm` | GET/PUT/DELETE | VM rightsizing thresholds (`vm` plugin) |
| Settings | `/recommendations/openshift/settings/vm/terms` | GET/PUT/DELETE | VM term windows |
| Settings | `/recommendations/openshift/settings/idle-detection` | GET/PUT/DELETE | Idle/zombie classification thresholds |
| Settings | `/recommendations/openshift/settings/quota` | GET/PUT/DELETE | ResourceQuota headroom and risk thresholds (`quota` plugin) |
| Settings | `/recommendations/openshift/settings/cluster-quota` | GET/PUT/DELETE | ClusterResourceQuota thresholds (`cluster-quota` plugin) |
| Settings | `/recommendations/openshift/settings/business-hours` | GET/PUT/DELETE | Org default business-hours schedule (PUT returns 202, triggers reship) |
| Settings | `/recommendations/openshift/settings/business-hours/clusters/{cluster_id}` | GET/PUT/DELETE | Cluster override |
| Settings | `/recommendations/openshift/settings/business-hours/clusters/{cluster_id}/namespaces/{namespace}` | GET/PUT/DELETE | Namespace override (most specific wins) |
| Settings | `/recommendations/openshift/settings/business-hours/effective` | GET | Resolved schedule for `cluster_id` and `namespace` query params (`resolved_from`: org, cluster, namespace, or none) |
| VMs | `/recommendations/openshift/vm` | GET | VM rightsizing list — filters include `filter[is_idle]`, `filter[is_abandoned]`, `filter[engine]`, `filter[term]`; `format=csv` or `Accept: text/csv` for export; `savings` object or `null` (not code **25**) |
| VMs | `/recommendations/openshift/vm/detail` | GET | VM detail with daily digests |
| VMs | `/recommendations/openshift/vms/{vm_name}/history` | GET | VM recommendation history — `format=csv` supported |
| VMs | `/recommendations/openshift/instance-types` | GET | Available instance types and preferences per cluster (`cluster_uuid` required) |
| VMs | `/recommendations/openshift/notification-codes` | GET | Filter `filter[plugin]=vm` for codes **18**–**69** |

## Non-OpenAPI routes

These operational endpoints are served by the ROS API and processor binaries but are **not** part of `openapi.json`:

| Path | Method | Description |
|------|--------|-------------|
| `/status` | GET | Trivial liveness — always 200 when process is up |
| `/healthz` | GET | Deep liveness — goroutine count, GC pause thresholds |
| `/readyz` | GET | Readiness — PostgreSQL ping; optional Kafka/S3 when `ROS_READINESS_CHECK_*` enabled |
| `/internal/tags/sync` | POST | Koku tag push sync (full-replace per org) |
| `/internal/tags/status` | GET | Tag sync freshness (`synced_at`, enabled key catalog) |
| `/internal/recalculate-savings` | POST | Trigger savings recalculation after cost model updates |
| `/internal/backfill-gpu-timeslicing` | POST | Backfill GPU time-slicing recommendations from existing digest data |

See [Monitoring](monitoring.md) for probe configuration and [Configuration — Tag Sync](configuration.md#tag-sync) for internal tag routes.

When `ROS_SETTINGS_LOCKED=true`, settings GET responses include `settings_locked: true`; PUT/DELETE
return `403`. See [Configuration — Global Settings Lock](configuration.md#global-settings-lock).
`ROS_SETTINGS_LOCKED_BUSINESS_HOURS` applies the same lock to business-hours settings only.

Container and namespace **detail** responses and **list** responses (when business-hours enrichment is enabled) may include `recommendations.recommendation_terms.*.recommendation_engines.{cost,performance}.business_hours` (schedule-weighted sizing; omitted when no schedule applies). Use the **effective** settings GET for timezone, `schedule`, `off_hours_weight`, and `enabled`.

## Authentication

All endpoints require the `x-rh-identity` header (base64-encoded JSON identity).
See the [API Versioning](architecture/api-versioning.md) doc for compatibility policy.

Query parameters use Koku bracket notation (`filter[project]`, `order_by[field]`); see
[Query Parameters](plugin-reference/query-parameters.md). **mTLS** is the planned auth upgrade
for on-prem service accounts; bracket syntax is unchanged.

## PVC right-sizing (list and detail)

The PVC plugin (`pvc`) exposes list and detail endpoints documented in
[plugin-reference/pvc.md](plugin-reference/pvc.md) and
[features/pvc-rightsizing.md](features/pvc-rightsizing.md).

**List response highlights** (default term `medium`):

| Field | When present | Description |
|-------|----------------|-------------|
| `estimated_monthly_savings` | Oversized/orphaned when Masu rates available | Structured `{value, units}` monthly savings |
| `idle_since`, `idle_duration_days` | `recommendation_type=orphaned` | First zero-usage date and days idle |
| `days_to_full`, `growth_bytes_per_day` | Near-full/oversized with growth projection | Capacity runway from WLS trend |
| `mounted_by` | When storage CSV reported a pod | Last-seen mounting pod name |
| `vm_name` | KubeVirt VM disk (`virt-launcher-*` + operator `vm_name` column) | Linked VM name |

**Notification codes** (catalog: `GET .../notification-codes?filter[plugin]=pvc`):

| Code | Severity | Meaning |
|------|----------|---------|
| 20 | WARNING | Zero usage across all intervals |
| 25 | INFO | No cost data — savings not computed |
| 29 | INFO | Capacity exceeds sustained usage — consider shrinking |
| 30 | WARNING | Usage approaching capacity or growth alert |

**Fleet rollup:** `GET /recommendations/openshift/savings-summary` includes `by_plugin.pvc`
alongside container, node, VM, and other plugins. PVC totals filter by `term` only (not `engine`).
Snapshot totals are **term-independent** (all snapshot recommendations are summed regardless of `term`).
Container, node, and VM totals honor both `engine` and `term`.

## VM (OpenShift Virtualization) recommendations

The `vm` plugin exposes list, detail, history, and instance-type endpoints documented in
[plugin-reference/vm.md](plugin-reference/vm.md) and
[features/virtual-machines.md](features/virtual-machines.md).

**List filters:** `filter[cluster]`, `filter[namespace]` / `filter[project]`, `filter[vm_name]`,
`filter[term]`, `filter[engine]`, `filter[is_idle]`, `filter[is_abandoned]`, `filter[is_oversized]`,
`filter[has_gpu]`, `filter[is_network_bound]`, `filter[guest_os]`, `filter[tag:<key>]` (when tags enabled).

**Savings:** `savings` is `{value, units}` when rates exist at ingestion, or JSON **`null`** when
`ROS_SAVINGS_ESTIMATES_ENABLED=false` or Masu has no rates — VMs do **not** emit notification code **25**.

**CSV export:** `GET .../vm?format=csv` and `GET .../vms/{vm_name}/history?format=csv` (or `Accept: text/csv`).

**Notification codes** (catalog: `GET .../notification-codes?filter[plugin]=vm`):

| Range | Topics |
|-------|--------|
| 18–19 | Idle, oversized |
| 37–54 | Sizing, disk, guest agent, GPU |
| 55–59 | Network-bound, vGPU, I/O pattern |
| 60–63 | Placement, NUMA |
| 64 | Power-off candidate |
| 65–69 | Network QoS, storage tiering |

**Business hours:** not applicable to VMs.
